package agent

import (
	"gin-dify-saas/server/internal/platform/httpx"
	"gin-dify-saas/server/internal/platform/requestctx"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(c *gin.Context) {
	identity, ok := requestctx.Get(c)
	if !ok {
		httpx.Error(c, 401, 14010, "missing identity")
		return
	}
	items, err := h.service.List(c.Request.Context(), identity.TenantID)
	if err != nil {
		httpx.Error(c, 500, 15100, "failed to list agents")
		return
	}
	httpx.OK(c, items)
}
