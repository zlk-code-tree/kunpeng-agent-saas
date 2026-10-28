package integration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gin-dify-saas/server/internal/platform/ids"
	"gin-dify-saas/server/internal/platform/secretbox"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db  *pgxpool.Pool
	box *secretbox.Box
}

type CreateInstanceInput struct {
	Name    string `json:"name" binding:"required,min=2,max=100"`
	BaseURL string `json:"baseUrl" binding:"required,url"`
	APIKey  string `json:"apiKey" binding:"required,min=8"`
}

type BindAppInput struct {
	AgentVersionID string `json:"agentVersionId" binding:"required,uuid"`
	DifyInstanceID string `json:"difyInstanceId" binding:"required,uuid"`
	UpstreamAppID  string `json:"upstreamAppId"`
	APIKey         string `json:"apiKey" binding:"required,min=8"`
}

type Instance struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	BaseURL   string    `json:"baseUrl"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

func NewService(db *pgxpool.Pool, box *secretbox.Box) *Service {
	return &Service{db: db, box: box}
}

func (s *Service) CreateInstance(ctx context.Context, tenantID string, input CreateInstanceInput) (Instance, error) {
	id, err := ids.New()
	if err != nil {
		return Instance{}, err
	}
	encrypted, err := s.box.Seal(strings.TrimSpace(input.APIKey))
	if err != nil {
		return Instance{}, err
	}
	item := Instance{ID: id, Name: strings.TrimSpace(input.Name), BaseURL: strings.TrimRight(strings.TrimSpace(input.BaseURL), "/"), Status: "active", CreatedAt: time.Now().UTC()}
	_, err = s.db.Exec(ctx, `
		INSERT INTO dify_instances (id, tenant_id, name, base_url, encrypted_credentials, status)
		VALUES ($1, $2, $3, $4, $5, 'active')
	`, item.ID, tenantID, item.Name, item.BaseURL, encrypted)
	if err != nil {
		return Instance{}, fmt.Errorf("create dify instance: %w", err)
	}
	return item, nil
}

func (s *Service) ListInstances(ctx context.Context, tenantID string) ([]Instance, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, name, base_url, status, created_at
		FROM dify_instances
		WHERE tenant_id = $1 OR is_shared = true
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list dify instances: %w", err)
	}
	defer rows.Close()
	items := make([]Instance, 0)
	for rows.Next() {
		var item Instance
		if err := rows.Scan(&item.ID, &item.Name, &item.BaseURL, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) BindApp(ctx context.Context, tenantID string, input BindAppInput) error {
	id, err := ids.New()
	if err != nil {
		return err
	}
	encrypted, err := s.box.Seal(strings.TrimSpace(input.APIKey))
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Dify app binding: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE dify_app_bindings SET status='replaced', updated_at=NOW()
		WHERE tenant_id=$1 AND agent_version_id=$2 AND status='active'
	`, tenantID, input.AgentVersionID); err != nil {
		return fmt.Errorf("replace Dify app binding: %w", err)
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO dify_app_bindings
			(id, tenant_id, agent_version_id, dify_instance_id, upstream_app_id, encrypted_api_key, status)
		SELECT $1, $2, $3, i.id, $5, $6, 'active'
		FROM dify_instances i
		WHERE i.id = $4 AND (i.tenant_id = $2 OR i.is_shared = true)
	`, id, tenantID, input.AgentVersionID, input.DifyInstanceID, strings.TrimSpace(input.UpstreamAppID), encrypted)
	if err != nil {
		return fmt.Errorf("bind dify app: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("dify instance not found")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Dify app binding: %w", err)
	}
	return nil
}
