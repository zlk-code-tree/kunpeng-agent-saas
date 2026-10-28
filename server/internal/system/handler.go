package system

import (
	"context"
	"time"

	"gin-dify-saas/server/internal/installation"
	"gin-dify-saas/server/internal/platform/httpx"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	installations *installation.Service
	db            *pgxpool.Pool
	redis         *redis.Client
}

// NewHandler 创建系统健康检查与能力查询处理器。
func NewHandler(installations *installation.Service, db *pgxpool.Pool, redisClient *redis.Client) *Handler {
	return &Handler{installations: installations, db: db, redis: redisClient}
}

// Health 只表示 HTTP 进程仍存活，不探测外部依赖。
func (h *Handler) Health(c *gin.Context) {
	httpx.OK(c, map[string]string{"status": "ok"})
}

// Ready 检查数据库和 Redis，只有核心依赖均可用时才允许接收业务流量。
func (h *Handler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.db.Ping(ctx); err != nil {
		httpx.Error(c, 503, 15020, "database is not ready")
		return
	}
	if err := h.redis.Ping(ctx).Err(); err != nil {
		httpx.Error(c, 503, 15021, "redis is not ready")
		return
	}
	httpx.OK(c, map[string]string{"status": "ready"})
}

// Capabilities 根据安装模式返回前端可展示和可调用的平台能力。
func (h *Handler) Capabilities(c *gin.Context) {
	status, err := h.installations.Status(c.Request.Context())
	if err != nil {
		httpx.Error(c, 500, 15001, "failed to load capabilities")
		return
	}
	isMulti := status.Mode == "multi"
	httpx.OK(c, map[string]any{
		"installed": status.Installed,
		"appMode":   status.Mode,
		"features": map[string]bool{
			"tenantCreation":  isMulti,
			"tenantSwitching": isMulti,
			"billing":         true,
			"payment":         false,
			"enterpriseDify":  true,
			"modelRouting":    true,
			"toolGateway":     true,
			"providerCosting": true,
			"tenantExport":    true,
		},
	})
}
