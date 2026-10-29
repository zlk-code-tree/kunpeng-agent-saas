package toolgateway

import (
	"errors"
	"strings"

	"gin-dify-saas/server/internal/transport/httpx"
	"gin-dify-saas/server/internal/transport/requestctx"

	"github.com/gin-gonic/gin"
)

type ToolgatewayHandler struct{ service *ToolgatewayService }

// NewToolgatewayHandler 创建受控工具目录、授权、票据和调用接口处理器。
func NewToolgatewayHandler(service *ToolgatewayService) *ToolgatewayHandler {
	return &ToolgatewayHandler{service: service}
}

// Create 创建新的平台工具及其首个版本。
func (h *ToolgatewayHandler) Create(c *gin.Context) {
	var input CreateToolInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14800, "invalid tool input")
		return
	}
	item, err := h.service.CreateTool(c.Request.Context(), input)
	if err != nil {
		httpx.Error(c, 400, 14801, err.Error())
		return
	}
	httpx.Created(c, item)
}

// List 返回工具目录的最新有效版本。
func (h *ToolgatewayHandler) List(c *gin.Context) {
	items, err := h.service.ListTools(c.Request.Context())
	if err != nil {
		httpx.Error(c, 500, 15800, "failed to list tools")
		return
	}
	httpx.OK(c, items)
}

// Grant 配置租户对工具的调用授权。
func (h *ToolgatewayHandler) Grant(c *gin.Context) {
	var input GrantInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14802, "invalid tool grant")
		return
	}
	if err := h.service.Grant(c.Request.Context(), c.Param("id"), input); err != nil {
		httpx.Error(c, 500, 15801, "failed to grant tool")
		return
	}
	httpx.OK(c, map[string]bool{"success": true})
}

// Ticket 为当前用户的一次运行签发短期工具调用票据。
func (h *ToolgatewayHandler) Ticket(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input TicketInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14803, "invalid tool ticket input")
		return
	}
	ticket, expiresAt, err := h.service.IssueTicket(c.Request.Context(), current.TenantID, current.UserID, c.Param("id"), input.ToolCode)
	if err != nil {
		httpx.Error(c, 403, 14804, err.Error())
		return
	}
	httpx.OK(c, map[string]any{"ticket": ticket, "expiresAt": expiresAt})
}

// Invoke 使用运行绑定票据调用工具；该接口不依赖普通用户 JWT。
func (h *ToolgatewayHandler) Invoke(c *gin.Context) {
	ticket := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Tool "))
	var input InvokeInput
	if ticket == "" || c.ShouldBindJSON(&input) != nil {
		httpx.Error(c, 401, 14805, "tool ticket is required")
		return
	}
	result, err := h.service.Invoke(c.Request.Context(), ticket, input.Arguments)
	if errors.Is(err, ErrInvalidTicket) || errors.Is(err, ErrDenied) {
		httpx.Error(c, 403, 14804, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 502, 15802, err.Error())
		return
	}
	httpx.OK(c, result)
}
