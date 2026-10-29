package operations

import (
	"gin-dify-saas/server/internal/transport/httpx"
	"gin-dify-saas/server/internal/transport/requestctx"

	"github.com/gin-gonic/gin"
)

type OperationsHandler struct{ service *OperationsService }

// NewOperationsHandler 创建平台运维接口处理器。
func NewOperationsHandler(service *OperationsService) *OperationsHandler {
	return &OperationsHandler{service: service}
}

// ExportTenant 处理平台管理员发起的租户快照导出。
func (h *OperationsHandler) ExportTenant(c *gin.Context) {
	current, _ := requestctx.Get(c)
	result, err := h.service.ExportTenant(c.Request.Context(), c.Param("id"), current.UserID)
	if err != nil {
		httpx.Error(c, 500, 15900, err.Error())
		return
	}
	httpx.OK(c, result)
}
