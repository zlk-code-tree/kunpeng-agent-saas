package server

import (
	"net/http"
	"strings"
	"time"

	"gin-dify-saas/server/internal/agent"
	"gin-dify-saas/server/internal/config"
	"gin-dify-saas/server/internal/execution"
	"gin-dify-saas/server/internal/identity"
	"gin-dify-saas/server/internal/installation"
	"gin-dify-saas/server/internal/integration"
	"gin-dify-saas/server/internal/integration/dify"
	"gin-dify-saas/server/internal/knowledge"
	"gin-dify-saas/server/internal/platform/ids"
	"gin-dify-saas/server/internal/platform/objectstore"
	"gin-dify-saas/server/internal/platform/secretbox"
	"gin-dify-saas/server/internal/system"
	"gin-dify-saas/server/internal/tenant"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func NewRouter(cfg config.Config, db *pgxpool.Pool, redisClient *redis.Client) *gin.Engine {
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(requestID(), gin.Recovery(), requestLogger(), cors(cfg.CORSOrigins))

	installationService := installation.NewService(db, cfg.Mode)
	installationHandler := installation.NewHandler(installationService)
	tokenManager := identity.NewTokenManager(cfg.JWT.Secret, cfg.JWT.TTL)
	identityService := identity.NewService(db, tokenManager)
	identityHandler := identity.NewHandler(identityService)
	tenantHandler := tenant.NewHandler(tenant.NewService(db, tokenManager, cfg.Mode))
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
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	executionHandler := execution.NewHandler(execution.NewService(db, queue))
	systemHandler := system.NewHandler(installationService, db, redisClient)

	router.GET("/healthz", systemHandler.Health)
	router.GET("/readyz", systemHandler.Ready)

	api := router.Group("/api/v1")
	api.GET("/install/status", installationHandler.Status)
	api.POST("/install", installationHandler.Setup)
	api.GET("/system/capabilities", systemHandler.Capabilities)
	api.POST("/auth/login", identityHandler.Login)
	api.POST("/auth/register", tenantHandler.Register)

	secured := api.Group("")
	secured.Use(identity.Middleware(db, tokenManager))
	secured.POST("/auth/logout", identityHandler.Logout)
	secured.GET("/auth/codes", identityHandler.Codes)
	secured.GET("/user/info", identityHandler.UserInfo)
	secured.GET("/agents", agentHandler.List)
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

	admin := secured.Group("/admin")
	admin.Use(identity.RequireRoles("owner", "admin"))
	admin.GET("/dify-instances", integrationHandler.ListInstances)
	admin.POST("/dify-instances", integrationHandler.CreateInstance)
	admin.POST("/dify-bindings", integrationHandler.BindApp)

	return router
}

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
