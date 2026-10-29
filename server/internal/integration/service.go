package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gin-dify-saas/server/internal/platform/ids"
	"gin-dify-saas/server/internal/platform/secretbox"

	"github.com/jackc/pgx/v5/pgxpool"
)

type IntegrationService struct {
	db         *pgxpool.Pool
	box        *secretbox.Box
	httpClient *http.Client
}

// NewIntegrationService 创建外部集成、模型供应商和 Dify 实例管理服务。
func NewIntegrationService(db *pgxpool.Pool, box *secretbox.Box) *IntegrationService {
	return &IntegrationService{db: db, box: box, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

// CreateInstance 创建共享或租户独享的 Dify 实例，并加密保存管理凭据。
func (s *IntegrationService) CreateInstance(ctx context.Context, tenantID string, input CreateInstanceInput) (Instance, error) {
	id, err := ids.New()
	if err != nil {
		return Instance{}, err
	}
	encrypted, err := s.box.Seal(strings.TrimSpace(input.APIKey))
	if err != nil {
		return Instance{}, err
	}
	deploymentType := input.DeploymentType
	if deploymentType == "" {
		deploymentType = "dedicated"
	}
	// shared 实例是所有租户可见的平台资源，dedicated 实例保留租户归属。
	// 执行路由仍优先选择租户专属绑定，共享基础设施不代表共享应用凭据或业务数据。
	item := Instance{ID: id, Name: strings.TrimSpace(input.Name), BaseURL: strings.TrimRight(strings.TrimSpace(input.BaseURL), "/"), Status: "active", DeploymentType: deploymentType, HealthStatus: "unknown", CredentialVersion: 1, CreatedAt: time.Now().UTC()}
	_, err = s.db.Exec(ctx, `
		INSERT INTO dify_instances (id, tenant_id, name, base_url, encrypted_credentials, status,deployment_type,is_shared)
		VALUES ($1, $2, $3, $4, $5, 'active',$6,$7)
	`, item.ID, tenantID, item.Name, item.BaseURL, encrypted, deploymentType, deploymentType == "shared")
	if err != nil {
		return Instance{}, fmt.Errorf("create dify instance: %w", err)
	}
	return item, nil
}

// ListInstances 返回当前租户自有实例和平台共享实例。
func (s *IntegrationService) ListInstances(ctx context.Context, tenantID string) ([]Instance, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text,name,base_url,status,deployment_type,health_status,last_health_check_at,
		       consecutive_failures,credential_version,created_at
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
		if err := rows.Scan(&item.ID, &item.Name, &item.BaseURL, &item.Status, &item.DeploymentType, &item.HealthStatus, &item.LastHealthCheckAt, &item.ConsecutiveFailures, &item.CredentialVersion, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CheckInstance 调用 Dify /info 接口检查连通性和凭据，并更新实例健康状态。
func (s *IntegrationService) CheckInstance(ctx context.Context, tenantID, instanceID string) (Instance, error) {
	var item Instance
	var encrypted []byte
	err := s.db.QueryRow(ctx, `
		SELECT id::text,name,base_url,status,deployment_type,health_status,last_health_check_at,
		       consecutive_failures,credential_version,created_at,encrypted_credentials
		FROM dify_instances WHERE id=$1 AND (tenant_id=$2 OR is_shared=TRUE)
	`, instanceID, tenantID).Scan(&item.ID, &item.Name, &item.BaseURL, &item.Status, &item.DeploymentType, &item.HealthStatus, &item.LastHealthCheckAt, &item.ConsecutiveFailures, &item.CredentialVersion, &item.CreatedAt, &encrypted)
	if err != nil {
		return Instance{}, err
	}
	key, err := s.box.Open(encrypted)
	if err != nil {
		return Instance{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(item.BaseURL, "/")+"/info", nil)
	if err != nil {
		return Instance{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	// 带认证的 /info 同时验证网络和凭据。4xx 表示服务可达但配置异常，标记为 degraded；
	// 网络错误或 5xx 标记为 unavailable，并从新任务路由中排除。
	response, requestErr := s.httpClient.Do(req)
	health := "healthy"
	if requestErr != nil {
		health = "unavailable"
	} else {
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode >= 500 {
			health = "unavailable"
		} else if response.StatusCode >= 400 {
			health = "degraded"
		}
	}
	checkedAt := time.Now().UTC()
	if health == "healthy" {
		item.ConsecutiveFailures = 0
	} else {
		item.ConsecutiveFailures++
	}
	item.HealthStatus = health
	item.LastHealthCheckAt = &checkedAt
	_, err = s.db.Exec(ctx, `
		UPDATE dify_instances SET health_status=$2,last_health_check_at=$3,
		consecutive_failures=$4,updated_at=NOW() WHERE id=$1
	`, item.ID, health, checkedAt, item.ConsecutiveFailures)
	return item, err
}

// CheckAll 批量检查全部有效 Dify 实例和模型供应商，供 Worker 定时任务调用。
func (s *IntegrationService) CheckAll(ctx context.Context) (int, int) {
	// 先读取目标快照并关闭结果集，再执行慢速远程请求，避免长时间占用数据库连接。
	rows, err := s.db.Query(ctx, `SELECT id::text,COALESCE(tenant_id::text,(SELECT default_tenant_id::text FROM installation_settings LIMIT 1)) FROM dify_instances WHERE status='active'`)
	if err != nil {
		return 0, 0
	}
	type target struct{ id, tenantID string }
	targets := make([]target, 0)
	for rows.Next() {
		var item target
		if rows.Scan(&item.id, &item.tenantID) == nil {
			targets = append(targets, item)
		}
	}
	rows.Close()
	difyChecked := 0
	for _, item := range targets {
		if _, err := s.CheckInstance(ctx, item.tenantID, item.id); err == nil {
			difyChecked++
		}
	}
	providerRows, err := s.db.Query(ctx, `SELECT id::text,base_url,encrypted_credentials FROM model_providers WHERE status='active'`)
	if err != nil {
		return difyChecked, 0
	}
	providerChecked := 0
	for providerRows.Next() {
		var id, baseURL string
		var encrypted []byte
		if providerRows.Scan(&id, &baseURL, &encrypted) != nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
		if err != nil {
			continue
		}
		if len(encrypted) > 0 {
			if key, openErr := s.box.Open(encrypted); openErr == nil {
				req.Header.Set("Authorization", "Bearer "+key)
			}
		}
		response, requestErr := s.httpClient.Do(req)
		health := "healthy"
		if requestErr != nil {
			health = "unavailable"
		} else {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
			if response.StatusCode >= 500 {
				health = "unavailable"
			} else if response.StatusCode >= 400 {
				health = "degraded"
			}
		}
		_, _ = s.db.Exec(ctx, `UPDATE model_providers SET health_status=$2,last_health_check_at=NOW(),updated_at=NOW() WHERE id=$1`, id, health)
		providerChecked++
	}
	providerRows.Close()
	return difyChecked, providerChecked
}

// RotateInstanceCredential 原子替换租户 Dify 实例凭据并递增可审计的凭据版本。
func (s *IntegrationService) RotateInstanceCredential(ctx context.Context, tenantID, instanceID, apiKey string) error {
	encrypted, err := s.box.Seal(strings.TrimSpace(apiKey))
	if err != nil {
		return err
	}
	// 轮换时重置健康状态；后续健康检查决定实例何时重新进入可路由状态。
	result, err := s.db.Exec(ctx, `
		UPDATE dify_instances SET encrypted_credentials=$3,credential_version=credential_version+1,
		health_status='unknown',consecutive_failures=0,updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2
	`, instanceID, tenantID, encrypted)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("dify instance not found")
	}
	return nil
}

// CreateProvider 创建租户自有或平台共享的模型供应商，并加密保存 API Key。
func (s *IntegrationService) CreateProvider(ctx context.Context, tenantID string, input CreateProviderInput) (Provider, error) {
	id, _ := ids.New()
	var encrypted []byte
	var err error
	if strings.TrimSpace(input.APIKey) != "" {
		encrypted, err = s.box.Seal(strings.TrimSpace(input.APIKey))
		if err != nil {
			return Provider{}, err
		}
	}
	item := Provider{ID: id, Code: strings.ToLower(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name), ProviderType: input.ProviderType, BaseURL: strings.TrimRight(strings.TrimSpace(input.BaseURL), "/"), Status: "active", Shared: input.Shared, HealthStatus: "unknown", CreatedAt: time.Now().UTC()}
	ownerTenantID := any(tenantID)
	if input.Shared {
		// tenant_id 为 NULL 表示平台共享；租户供应商始终保留所属租户，其他租户无法查询。
		ownerTenantID = nil
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO model_providers (id,tenant_id,code,name,provider_type,base_url,encrypted_credentials,status,is_shared)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'active',$8)
	`, item.ID, ownerTenantID, item.Code, item.Name, item.ProviderType, item.BaseURL, encrypted, item.Shared)
	return item, err
}

// ListProviders 返回当前租户可使用的自有和共享模型供应商。
func (s *IntegrationService) ListProviders(ctx context.Context, tenantID string) ([]Provider, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text,code,name,provider_type,base_url,status,is_shared,health_status,last_health_check_at,created_at
		FROM model_providers WHERE tenant_id=$1 OR is_shared=TRUE ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Provider, 0)
	for rows.Next() {
		var item Provider
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.ProviderType, &item.BaseURL, &item.Status, &item.Shared, &item.HealthStatus, &item.LastHealthAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SetModelRoute 为智能体版本增加一条按优先级选择的模型路由。
func (s *IntegrationService) SetModelRoute(ctx context.Context, tenantID string, input SetModelRouteInput) error {
	id, _ := ids.New()
	priority := input.Priority
	if priority == 0 {
		priority = 100
	}
	config, _ := json.Marshal(input.Config)
	result, err := s.db.Exec(ctx, `
		INSERT INTO agent_model_routes (id,tenant_id,agent_version_id,provider_id,model,priority,status,config)
		SELECT $1,$2,$3,p.id,$5,$6,'active',$7
		FROM model_providers p WHERE p.id=$4 AND p.status='active' AND (p.tenant_id=$2 OR p.is_shared=TRUE)
		ON CONFLICT DO NOTHING
	`, id, tenantID, input.AgentVersionID, input.ProviderID, strings.TrimSpace(input.Model), priority, config)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("provider unavailable or route already exists")
	}
	return nil
}

// SetModelPrice 为指定供应商模型发布一个新的成本价格版本。
func (s *IntegrationService) SetModelPrice(ctx context.Context, tenantID, providerID string, input SetModelPriceInput) error {
	id, _ := ids.New()
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		currency = "CNY"
	}
	// 价格采用只追加版本；历史成本记录保留原价格版本 ID，价格变化后仍能复现历史成本和毛利。
	result, err := s.db.Exec(ctx, `
		INSERT INTO model_price_versions (
			id,provider_id,model,version,input_per_1000,output_per_1000,currency,effective_at,status
		)
		SELECT $1,p.id,$3::varchar,COALESCE((SELECT MAX(version)+1 FROM model_price_versions WHERE provider_id=p.id AND model=$3::varchar),1),
		       $4::numeric,$5::numeric,$6,NOW(),'active'
		FROM model_providers p WHERE p.id=$2 AND (p.tenant_id=$7 OR p.is_shared=TRUE)
	`, id, providerID, strings.TrimSpace(input.Model), input.InputPer1000, input.OutputPer1000, currency, tenantID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("model provider not found")
	}
	return nil
}

// BindApp 将租户的智能体版本绑定到一个 Dify 应用，并替换旧的租户专属绑定。
func (s *IntegrationService) BindApp(ctx context.Context, tenantID string, input BindAppInput) error {
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
	defer func() { _ = tx.Rollback(ctx) }()
	// 替换旧绑定和新增绑定在同一事务中完成，读取方始终只能看到一个有效租户绑定；
	// 平台共享的兜底绑定不会被修改。
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
