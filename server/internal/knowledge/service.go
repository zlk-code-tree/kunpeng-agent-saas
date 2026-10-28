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

func NewService(db *pgxpool.Pool, store *objectstore.Store, box *secretbox.Box, difyClient *dify.Client) *Service {
	return &Service{db: db, store: store, box: box, dify: difyClient}
}

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
	objectKey := tenantID + "/" + knowledgeBaseID + "/" + fileID
	if err := s.store.Put(ctx, objectKey, contentType, size, reader); err != nil {
		return Document{}, err
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return Document{}, err
	}

	indexingStatus := "waiting"
	var upstreamDocumentID, upstreamBatchID string
	if baseURL != "" && upstreamDatasetID != "" && len(encrypted) > 0 {
		if apiKey, openErr := s.box.Open(encrypted); openErr == nil {
			if difyDocumentID, batchID, uploadErr := s.dify.UploadDocument(ctx, baseURL, apiKey, upstreamDatasetID, name, reader); uploadErr == nil {
				upstreamDocumentID, upstreamBatchID, indexingStatus = difyDocumentID, batchID, "indexing"
			}
		}
	}

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

func nullValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}
