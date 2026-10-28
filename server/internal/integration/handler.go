package integration

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

func (h *Handler) ListInstances(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	items, err := h.service.ListInstances(c.Request.Context(), identity.TenantID)
	if err != nil {
		httpx.Error(c, 500, 15300, "failed to list Dify instances")
		return
	}
	httpx.OK(c, items)
}

func (h *Handler) CreateInstance(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	var input CreateInstanceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14300, "invalid Dify instance input")
		return
	}
	item, err := h.service.CreateInstance(c.Request.Context(), identity.TenantID, input)
	if err != nil {
		httpx.Error(c, 500, 15301, "failed to create Dify instance")
		return
	}
	httpx.Created(c, item)
}

func (h *Handler) BindApp(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	var input BindAppInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14302, "invalid Dify app binding")
		return
	}
	if err := h.service.BindApp(c.Request.Context(), identity.TenantID, input); err != nil {
		httpx.Error(c, 500, 15302, "failed to bind Dify app")
		return
	}
	httpx.Created(c, map[string]bool{"success": true})
}
