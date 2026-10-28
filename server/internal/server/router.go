package server

import (
	"net/http"
	"strings"
	"time"

	"gin-dify-saas/server/internal/agent"
	"gin-dify-saas/server/internal/billing"
	"gin-dify-saas/server/internal/config"
	"gin-dify-saas/server/internal/execution"
	"gin-dify-saas/server/internal/identity"
	"gin-dify-saas/server/internal/installation"
	"gin-dify-saas/server/internal/integration"
	"gin-dify-saas/server/internal/integration/dify"
	"gin-dify-saas/server/internal/knowledge"
	"gin-dify-saas/server/internal/operations"
	"gin-dify-saas/server/internal/platform/ids"
	"gin-dify-saas/server/internal/platform/objectstore"
	"gin-dify-saas/server/internal/platform/observability"
	"gin-dify-saas/server/internal/platform/secretbox"
	"gin-dify-saas/server/internal/system"
	"gin-dify-saas/server/internal/tenant"
	"gin-dify-saas/server/internal/toolgateway"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// NewRouter 组装全部领域服务、中间件和公开/认证/平台管理路由。
func NewRouter(cfg config.Config, db *pgxpool.Pool, redisClient *redis.Client) *gin.Engine {
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	metrics := observability.NewMetrics()
	router.Use(requestID(), gin.Recovery(), metrics.Middleware(), requestLogger(), cors(cfg.CORSOrigins))

	installationService := installation.NewService(db, cfg.Mode)
	installationHandler := installation.NewHandler(installationService)
	tokenManager := identity.NewTokenManager(cfg.JWT.Secret, cfg.JWT.TTL)
	identityService := identity.NewService(db, tokenManager)
	identityHandler := identity.NewHandler(identityService)
	tenantHandler := tenant.NewHandler(tenant.NewService(db, tokenManager, cfg.Mode))
	billingService := billing.NewService(db)
	billingHandler := billing.NewHandler(billingService)
	agentHandler := agent.NewHandler(agent.NewService(db))
	credentialBox, err := secretbox.New(cfg.CredentialKey)
	if err != nil {
		panic(err)
	}
	store, err := objectstore.New(cfg.Storage)
	if err != nil {
		panic(err)
	}
	knowledgeHandler := knowledge.NewHandler(knowledge.NewService(db, store, credentialBox, dify.NewClient(cfg.Dify.Timeout)))
	integrationHandler := integration.NewHandler(integration.NewService(db, credentialBox))
	toolHandler := toolgateway.NewHandler(toolgateway.NewService(db, credentialBox, cfg.JWT.Secret, cfg.Environment != "production"))
	operationsHandler := operations.NewHandler(operations.NewService(db))
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	executionHandler := execution.NewHandler(execution.NewService(db, queue, billingService))
	systemHandler := system.NewHandler(installationService, db, redisClient)

	router.GET("/healthz", systemHandler.Health)
	router.GET("/readyz", systemHandler.Ready)
	router.GET("/metrics", metrics.Handler)

	api := router.Group("/api/v1")
	api.GET("/install/status", installationHandler.Status)
	api.POST("/install", installationHandler.Setup)
	api.GET("/system/capabilities", systemHandler.Capabilities)
	api.POST("/auth/login", identityHandler.Login)
	api.POST("/auth/register", tenantHandler.Register)
	// 工具调用故意不走普通 JWT：调用方是持有短期、运行绑定 HMAC 票据的智能体运行时。
	// Invoke 会自行校验签名并依据数据库实时鉴权。
	api.POST("/internal/tools/invoke", toolHandler.Invoke)

	secured := api.Group("")
	secured.Use(identity.Middleware(db, tokenManager))
	secured.POST("/auth/logout", identityHandler.Logout)
	secured.GET("/auth/codes", identityHandler.Codes)
	secured.GET("/user/info", identityHandler.UserInfo)
	secured.GET("/agents", agentHandler.List)
	secured.GET("/billing/account", billingHandler.CurrentAccount)
	secured.GET("/billing/plans", billingHandler.Plans)
	secured.GET("/billing/subscription", billingHandler.CurrentSubscription)
	secured.GET("/billing/ledger", billingHandler.Ledger)
	secured.GET("/tenants", tenantHandler.List)
	secured.POST("/tenants", tenantHandler.Create)
	secured.POST("/tenants/:id/switch", tenantHandler.Switch)
	secured.GET("/tenants/:id/members", tenantHandler.Members)
	secured.POST("/tenants/:id/invitations", tenantHandler.Invite)
	secured.POST("/tenant-invitations/accept", tenantHandler.AcceptInvitation)
	secured.DELETE("/tenants/:id/members/:userId", tenantHandler.RemoveMember)
	secured.POST("/tenants/:id/leave", tenantHandler.Leave)
	secured.POST("/tenants/:id/transfer-ownership", tenantHandler.TransferOwnership)
	secured.GET("/knowledge-bases", knowledgeHandler.List)
	secured.POST("/knowledge-bases", knowledgeHandler.Create)
	secured.GET("/knowledge-bases/:id/documents", knowledgeHandler.ListDocuments)
	secured.POST("/knowledge-bases/:id/documents", knowledgeHandler.Upload)
	secured.POST("/runs", executionHandler.Create)
	secured.GET("/runs/:id", executionHandler.Get)
	secured.POST("/runs/:id/cancel", executionHandler.Cancel)
	secured.GET("/runs/:id/events", executionHandler.Events)
	secured.POST("/runs/:id/tool-tickets", toolHandler.Ticket)

	// 平台管理权限高于租户 owner/admin。集成、全局工具、调账和导出接口必须使用
	// 身份中间件验证的安装级平台管理员标记。
	admin := secured.Group("/admin")
	admin.Use(identity.RequirePlatformAdmin())
	admin.GET("/dify-instances", integrationHandler.ListInstances)
	admin.POST("/dify-instances", integrationHandler.CreateInstance)
	admin.POST("/dify-instances/:id/health-check", integrationHandler.CheckInstance)
	admin.POST("/dify-instances/:id/rotate-credential", integrationHandler.RotateInstanceCredential)
	admin.POST("/dify-bindings", integrationHandler.BindApp)
	admin.GET("/model-providers", integrationHandler.ListProviders)
	admin.POST("/model-providers", integrationHandler.CreateProvider)
	admin.POST("/model-routes", integrationHandler.SetModelRoute)
	admin.POST("/model-providers/:id/prices", integrationHandler.SetModelPrice)
	admin.GET("/tools", toolHandler.List)
	admin.POST("/tools", toolHandler.Create)
	admin.POST("/tools/:id/grants", toolHandler.Grant)
	admin.POST("/billing/accounts/:id/adjustments", billingHandler.Adjust)
	admin.GET("/costs/summary", billingHandler.CostSummary)
	admin.POST("/reconciliation", billingHandler.Reconcile)
	admin.POST("/tenants/:id/export", operationsHandler.ExportTenant)

	return router
}

// requestLogger 输出包含请求 ID、方法、路径、状态和耗时的访问日志。
func requestLogger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return strings.Join([]string{
			param.TimeStamp.Format(time.RFC3339),
			param.Request.Header.Get("X-Request-ID"),
			param.Method,
			param.Path,
			param.StatusCodeColor() + http.StatusText(param.StatusCode) + param.ResetColor(),
			param.Latency.String(),
			"\n",
		}, " ")
	})
}

// requestID 接受合法的上游请求 ID，否则生成新 UUID，并在响应头中回传。
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if !validRequestID(requestID) {
			requestID, _ = ids.New()
		}
		c.Request.Header.Set("X-Request-ID", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// validRequestID 限制请求 ID 的长度和字符集，防止日志注入。
func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

// cors 只对配置白名单中的 Origin 返回跨域凭据许可，并统一处理预检请求。
func cors(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if _, ok := allowed[origin]; ok {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept-Language, X-Request-ID, Idempotency-Key, Last-Event-ID")
			c.Header("Access-Control-Expose-Headers", "X-Request-ID")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
