package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gin-dify-saas/server/internal/integration/dify"
	"gin-dify-saas/server/internal/platform/ids"
	"gin-dify-saas/server/internal/platform/objectstore"
	"gin-dify-saas/server/internal/platform/secretbox"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db    *pgxpool.Pool
	store *objectstore.Store
	box   *secretbox.Box
	dify  *dify.Client
}

type Document struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	ContentType    string    `json:"contentType"`
	SizeBytes      int64     `json:"sizeBytes"`
	IndexingStatus string    `json:"indexingStatus"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Item struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Visibility string    `json:"visibility"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

type CreateInput struct {
	Name       string `json:"name" binding:"required,min=2,max=100"`
	Visibility string `json:"visibility" binding:"required,oneof=private tenant"`
}

// NewService 创建本地对象存储与 Dify 数据集协同的知识库服务。
func NewService(db *pgxpool.Pool, store *objectstore.Store, box *secretbox.Box, difyClient *dify.Client) *Service {
	return &Service{db: db, store: store, box: box, dify: difyClient}
}

// List 返回租户共享知识库和当前用户拥有的私有知识库。
func (s *Service) List(ctx context.Context, tenantID, userID string) ([]Item, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, name, visibility, status, created_at
		FROM knowledge_bases
		WHERE tenant_id = $1 AND (visibility = 'tenant' OR owner_user_id = $2)
		ORDER BY created_at DESC
	`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("list knowledge bases: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Name, &item.Visibility, &item.Status, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan knowledge base: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Create 创建本地知识库，并尽力在可用 Dify 实例上创建对应数据集。
func (s *Service) Create(ctx context.Context, tenantID, userID string, input CreateInput) (Item, error) {
	id, err := ids.New()
	if err != nil {
		return Item{}, err
	}
	item := Item{ID: id, Name: strings.TrimSpace(input.Name), Visibility: input.Visibility, Status: "provisioning", CreatedAt: time.Now().UTC()}
	_, err = s.db.Exec(ctx, `
		INSERT INTO knowledge_bases (id, tenant_id, owner_user_id, name, visibility, status)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, item.ID, tenantID, userID, item.Name, item.Visibility, item.Status)
	if err != nil {
		return Item{}, fmt.Errorf("create knowledge base: %w", err)
	}

	// 优先选择租户自有 Dify，缺失时回退平台共享实例。Dify 不存在或临时不可用时，
	// 本地知识库保留 provisioning 状态，不能因外部集成故障丢失本地业务数据。
	var instanceID, baseURL string
	var encrypted []byte
	err = s.db.QueryRow(ctx, `
		SELECT id::text, base_url, encrypted_credentials FROM dify_instances
		WHERE status='active' AND (tenant_id=$1 OR is_shared=true)
		ORDER BY (tenant_id=$1) DESC, created_at LIMIT 1
	`, tenantID).Scan(&instanceID, &baseURL, &encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return Item{}, fmt.Errorf("select Dify instance: %w", err)
	}
	apiKey, err := s.box.Open(encrypted)
	if err != nil {
		return item, nil
	}
	datasetID, err := s.dify.CreateDataset(ctx, baseURL, apiKey, item.Name)
	if err != nil {
		return item, nil
	}
	_, err = s.db.Exec(ctx, `
		UPDATE knowledge_bases SET dify_instance_id=$2, upstream_dataset_id=$3, status='active', updated_at=NOW()
		WHERE id=$1
	`, item.ID, instanceID, datasetID)
	if err != nil {
		return Item{}, fmt.Errorf("save Dify dataset: %w", err)
	}
	item.Status = "active"
	return item, nil
}

// Upload 保存原始文件和文档元数据，并尽力把同一文件提交给 Dify 建立索引。
func (s *Service) Upload(ctx context.Context, tenantID, userID, knowledgeBaseID, name, contentType, checksum string, size int64, reader io.ReadSeeker) (Document, error) {
	var baseURL, upstreamDatasetID string
	var encrypted []byte
	if err := s.db.QueryRow(ctx, `
		SELECT COALESCE(i.base_url,''), COALESCE(i.encrypted_credentials,''::bytea), COALESCE(k.upstream_dataset_id,'')
		FROM knowledge_bases k LEFT JOIN dify_instances i ON i.id=k.dify_instance_id
		WHERE k.id=$1 AND k.tenant_id=$2 AND (k.owner_user_id=$3 OR k.visibility='tenant')
	`, knowledgeBaseID, tenantID, userID).Scan(&baseURL, &encrypted, &upstreamDatasetID); err != nil {
		return Document{}, fmt.Errorf("knowledge base is not available")
	}
	fileID, err := ids.New()
	if err != nil {
		return Document{}, err
	}
	documentID, err := ids.New()
	if err != nil {
		return Document{}, err
	}
	// 对象键显式包含租户和知识库前缀，即使多个租户上传同名文件也能保持物理隔离。
	objectKey := tenantID + "/" + knowledgeBaseID + "/" + fileID
	if err := s.store.Put(ctx, objectKey, contentType, size, reader); err != nil {
		return Document{}, err
	}
	// 对象存储会先消费文件流，转发给 Dify 前必须回到起点，因此调用方需要提供 io.ReadSeeker。
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return Document{}, err
	}

	// 本地存储是数据事实来源。Dify 上传失败时文档保持 waiting，等待后续重试，
	// 不能拒绝已经成功落入对象存储的文件。
	indexingStatus := "waiting"
	var upstreamDocumentID, upstreamBatchID string
	if baseURL != "" && upstreamDatasetID != "" && len(encrypted) > 0 {
		if apiKey, openErr := s.box.Open(encrypted); openErr == nil {
			if difyDocumentID, batchID, uploadErr := s.dify.UploadDocument(ctx, baseURL, apiKey, upstreamDatasetID, name, reader); uploadErr == nil {
				upstreamDocumentID, upstreamBatchID, indexingStatus = difyDocumentID, batchID, "indexing"
			}
		}
	}

	// 文件元数据和逻辑文档在同一事务提交，避免查询到没有对象存储引用的文档。
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO files (id, tenant_id, owner_user_id, object_key, original_name, content_type, size_bytes, checksum_sha256)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, fileID, tenantID, userID, objectKey, name, contentType, size, checksum); err != nil {
		return Document{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO documents (id, tenant_id, knowledge_base_id, file_id, upstream_document_id, upstream_batch_id, indexing_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, documentID, tenantID, knowledgeBaseID, fileID, nullValue(upstreamDocumentID), nullValue(upstreamBatchID), indexingStatus); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return Document{ID: documentID, Name: name, ContentType: contentType, SizeBytes: size, IndexingStatus: indexingStatus, CreatedAt: time.Now().UTC()}, nil
}

// ListDocuments 在可见性规则下返回知识库文档及索引状态。
func (s *Service) ListDocuments(ctx context.Context, tenantID, userID, knowledgeBaseID string) ([]Document, error) {
	rows, err := s.db.Query(ctx, `
		SELECT d.id::text, f.original_name, f.content_type, f.size_bytes, d.indexing_status, d.created_at
		FROM documents d JOIN files f ON f.id=d.file_id
		JOIN knowledge_bases k ON k.id=d.knowledge_base_id
		WHERE d.tenant_id=$1 AND d.knowledge_base_id=$2 AND (k.owner_user_id=$3 OR k.visibility='tenant')
		ORDER BY d.created_at DESC
	`, tenantID, knowledgeBaseID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Document, 0)
	for rows.Next() {
		var item Document
		if err := rows.Scan(&item.ID, &item.Name, &item.ContentType, &item.SizeBytes, &item.IndexingStatus, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// nullValue 将空的上游标识转换为数据库 NULL。
func nullValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}
