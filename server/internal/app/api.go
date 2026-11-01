// Package app 负责应用进程的依赖装配和资源生命周期管理。
// 领域模块不依赖本包，因此未来拆分微服务时可以为每个服务提供独立装配入口。
package app

import (
	"fmt"
	"net/http"

	"gin-dify-saas/server/internal/administration"
	"gin-dify-saas/server/internal/agent"
	"gin-dify-saas/server/internal/billing"
	"gin-dify-saas/server/internal/config"
	"gin-dify-saas/server/internal/execution"
	"gin-dify-saas/server/internal/identity"
	"gin-dify-saas/server/internal/installation"
	"gin-dify-saas/server/internal/integration"
	"gin-dify-saas/server/internal/knowledge"
	"gin-dify-saas/server/internal/operations"
	"gin-dify-saas/server/internal/platform/dify"
	"gin-dify-saas/server/internal/platform/objectstore"
	"gin-dify-saas/server/internal/platform/observability"
	"gin-dify-saas/server/internal/platform/secretbox"
	"gin-dify-saas/server/internal/platform/security"
	"gin-dify-saas/server/internal/rbac"
	"gin-dify-saas/server/internal/system"
	"gin-dify-saas/server/internal/tenant"
	"gin-dify-saas/server/internal/toolgateway"
	"gin-dify-saas/server/internal/transport/httpapi"
	"gin-dify-saas/server/internal/transport/middleware"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// API 表示一个已完成装配的 HTTP 应用，并持有需要显式关闭的资源。
type API struct {
	handler http.Handler
	queue   *asynq.Client
}

// NewAPI 创建业务服务、处理器和路由。构造失败时不会启动 HTTP 监听。
func NewAPI(cfg config.Config, db *pgxpool.Pool, redisClient *redis.Client) (*API, error) {
	credentialBox, err := secretbox.New(cfg.CredentialKey)
	if err != nil {
		return nil, fmt.Errorf("create credential encryption: %w", err)
	}
	store, err := objectstore.New(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("create object store: %w", err)
	}

	tokens := security.NewTokenManager(cfg.JWT.Secret, cfg.JWT.TTL)
	queue := asynq.NewClient(asynq.RedisClientOpt{
		Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
	})

	// 领域服务只接收实际需要的基础设施或其他领域能力，避免依赖 HTTP 和启动逻辑。
	billingService := billing.NewBillingService(db)
	billingPort := billingAdapter{service: billingService}
	installationService := installation.NewInstallationService(db, billingPort, cfg.Mode)
	identityService := identity.NewIdentityService(db, tokens)
	tenantService := tenant.NewTenantService(db, tokens, billingPort, cfg.Mode)
	integrationService := integration.NewIntegrationService(db, credentialBox)

	router := httpapi.NewRouter(httpapi.Dependencies{
		Environment:  cfg.Environment,
		CORSOrigins:  cfg.CORSOrigins,
		Metrics:      observability.NewMetrics(),
		Authenticate: middleware.Auth(db, tokens),
		Installation: installation.NewInstallationHandler(installationService),
		Identity:     identity.NewIdentityHandler(identityService),
		Tenant:       tenant.NewTenantHandler(tenantService),
		Billing:      billing.NewBillingHandler(billingService),
		Agent:        agent.NewAgentHandler(agent.NewAgentService(db)),
		Knowledge: knowledge.NewKnowledgeHandler(knowledge.NewKnowledgeService(
			db, store, credentialBox, dify.NewClient(cfg.Dify.Timeout),
		)),
		Integration: integration.NewIntegrationHandler(integrationService),
		ToolGateway: toolgateway.NewToolgatewayHandler(toolgateway.NewToolgatewayService(
			db, credentialBox, cfg.JWT.Secret, cfg.Environment != "production",
		)),
		Execution:      execution.NewExecutionHandler(execution.NewExecutionService(db, queue, billingPort)),
		Operations:     operations.NewOperationsHandler(operations.NewOperationsService(db)),
		System:         system.NewSystemHandler(installationService, db, redisClient),
		Administration: administration.NewHandler(administration.NewService(db)),
		RBAC:           rbac.NewHandler(rbac.NewService(db)),
	})

	return &API{handler: router, queue: queue}, nil
}

// Handler 返回可交给 net/http Server 的应用处理器。
func (a *API) Handler() http.Handler { return a.handler }

// Close 关闭 API 进程持有的后台队列客户端。
func (a *API) Close() error {
	if a == nil || a.queue == nil {
		return nil
	}
	return a.queue.Close()
}
