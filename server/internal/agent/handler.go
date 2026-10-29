package agent

import (
	"gin-dify-saas/server/internal/transport/httpx"
	"gin-dify-saas/server/internal/transport/requestctx"

	"github.com/gin-gonic/gin"
)

type AgentHandler struct {
	service *AgentService
}

// NewAgentHandler 创建智能体目录接口处理器。
func NewAgentHandler(service *AgentService) *AgentHandler {
	return &AgentHandler{service: service}
}

// List 返回当前租户可使用的预置智能体。
func (h *AgentHandler) List(c *gin.Context) {
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
