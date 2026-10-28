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

func NewHandler(installations *installation.Service, db *pgxpool.Pool, redisClient *redis.Client) *Handler {
	return &Handler{installations: installations, db: db, redis: redisClient}
}

func (h *Handler) Health(c *gin.Context) {
	httpx.OK(c, map[string]string{"status": "ok"})
}

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
			"billing":         false,
			"payment":         false,
		},
	})
}
