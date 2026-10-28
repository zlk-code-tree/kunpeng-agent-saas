package operations

import (
	"gin-dify-saas/server/internal/platform/httpx"
	"gin-dify-saas/server/internal/platform/requestctx"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

// NewHandler 创建平台运维接口处理器。
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ExportTenant 处理平台管理员发起的租户快照导出。
func (h *Handler) ExportTenant(c *gin.Context) {
	current, _ := requestctx.Get(c)
	result, err := h.service.ExportTenant(c.Request.Context(), c.Param("id"), current.UserID)
	if err != nil {
		httpx.Error(c, 500, 15900, err.Error())
		return
	}
	httpx.OK(c, result)
}
