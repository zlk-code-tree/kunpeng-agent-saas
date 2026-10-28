package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db *pgxpool.Pool
}

type Item struct {
	ID              string         `json:"id"`
	AgentVersionID  string         `json:"agentVersionId"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Category        string         `json:"category"`
	Version         int            `json:"version"`
	ParameterSchema map[string]any `json:"parameterSchema"`
	Enabled         bool           `json:"enabled"`
	Configured      bool           `json:"configured"`
}

// NewService 创建预置智能体查询服务。
func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// List 返回当前租户可用的最新已发布智能体版本和配置状态。
func (s *Service) List(ctx context.Context, tenantID string) ([]Item, error) {
	// 智能体模板全局共享，但启用状态和 Dify 绑定按租户隔离。
	// 租户专属绑定优先于共享兜底绑定，与执行 Worker 的实际路由规则保持一致。
	rows, err := s.db.Query(ctx, `
		SELECT t.id::text, v.id::text, t.name, t.description, COALESCE(t.category, ''),
		       v.version, v.parameter_schema,
		       COALESCE(c.enabled, true), (b.id IS NOT NULL)
		FROM agent_templates t
		JOIN LATERAL (
			SELECT * FROM agent_versions av
			WHERE av.agent_template_id = t.id AND av.status = 'published'
			ORDER BY av.version DESC LIMIT 1
		) v ON true
		LEFT JOIN tenant_agent_configs c ON c.agent_template_id = t.id AND c.tenant_id = $1
		LEFT JOIN LATERAL (
			SELECT binding.id FROM dify_app_bindings binding
			WHERE binding.agent_version_id = v.id AND binding.status = 'active'
			  AND (binding.tenant_id = $1 OR binding.tenant_id IS NULL)
			ORDER BY (binding.tenant_id IS NOT NULL) DESC, binding.created_at DESC
			LIMIT 1
		) b ON true
		WHERE t.status = 'published'
		ORDER BY t.created_at
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		var schema []byte
		if err := rows.Scan(&item.ID, &item.AgentVersionID, &item.Name, &item.Description, &item.Category, &item.Version, &schema, &item.Enabled, &item.Configured); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		if err := json.Unmarshal(schema, &item.ParameterSchema); err != nil {
			return nil, fmt.Errorf("decode agent schema: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
