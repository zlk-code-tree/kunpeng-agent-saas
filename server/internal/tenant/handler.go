package tenant

import (
	"errors"
	"strings"

	"gin-dify-saas/server/internal/transport/httpx"
	"gin-dify-saas/server/internal/transport/requestctx"

	"github.com/gin-gonic/gin"
)

type TenantHandler struct{ service *TenantService }

// NewTenantHandler 创建租户与成员管理接口处理器。
func NewTenantHandler(service *TenantService) *TenantHandler {
	return &TenantHandler{service: service}
}

// Register 处理多租户模式的公开注册请求。
func (h *TenantHandler) Register(c *gin.Context) {
	var input RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14600, "invalid registration input")
		return
	}
	result, err := h.service.Register(c.Request.Context(), input)
	if h.writeError(c, err) {
		return
	}
	httpx.Created(c, result)
}

// List 返回当前用户加入的租户列表。
func (h *TenantHandler) List(c *gin.Context) {
	current, _ := requestctx.Get(c)
	items, err := h.service.List(c.Request.Context(), current.UserID, current.TenantID)
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, items)
}

// Create 创建新的企业租户空间。
func (h *TenantHandler) Create(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input TenantCreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14601, "invalid tenant input")
		return
	}
	item, err := h.service.Create(c.Request.Context(), current.UserID, input)
	if h.writeError(c, err) {
		return
	}
	httpx.Created(c, item)
}

// Switch 切换当前租户并返回绑定目标租户的新访问令牌。
func (h *TenantHandler) Switch(c *gin.Context) {
	current, _ := requestctx.Get(c)
	result, err := h.service.Switch(c.Request.Context(), current.UserID, c.Param("id"))
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, result)
}

// Members 返回当前已切换租户的成员列表。
func (h *TenantHandler) Members(c *gin.Context) {
	current, _ := requestctx.Get(c)
	if current.TenantID != c.Param("id") {
		httpx.Error(c, 403, 14603, "switch to this tenant before managing it")
		return
	}
	items, err := h.service.Members(c.Request.Context(), current.TenantID)
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, items)
}

// Invite 由租户所有者或管理员邀请成员。
func (h *TenantHandler) Invite(c *gin.Context) {
	current, _ := requestctx.Get(c)
	if current.TenantID != c.Param("id") || !hasAnyRole(current.Roles, "owner", "admin") {
		httpx.Error(c, 403, 14603, "permission denied")
		return
	}
	var input InviteInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14604, "invalid invitation input")
		return
	}
	item, err := h.service.Invite(c.Request.Context(), current.TenantID, current.UserID, input)
	if h.writeError(c, err) {
		return
	}
	httpx.Created(c, item)
}

// AcceptInvitation 接受属于当前用户的有效邀请。
func (h *TenantHandler) AcceptInvitation(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input struct {
		Token string `json:"token" binding:"required,min=32,max=256"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14606, "invitation token is required")
		return
	}
	item, err := h.service.AcceptInvitation(c.Request.Context(), current.UserID, input.Token)
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, item)
}

// RemoveMember 按角色权限移除租户成员。
func (h *TenantHandler) RemoveMember(c *gin.Context) {
	current, _ := requestctx.Get(c)
	if current.TenantID != c.Param("id") || !hasAnyRole(current.Roles, "owner", "admin") {
		httpx.Error(c, 403, 14603, "permission denied")
		return
	}
	// 中间件始终把当前租户角色放在首位；platform_admin 仅追加，不能替代租户内权限规则。
	err := h.service.RemoveMember(c.Request.Context(), current.TenantID, current.UserID, current.Roles[0], c.Param("userId"))
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, map[string]bool{"success": true})
}

// Leave 处理当前用户退出租户请求。
func (h *TenantHandler) Leave(c *gin.Context) {
	current, _ := requestctx.Get(c)
	if current.TenantID != c.Param("id") {
		httpx.Error(c, 403, 14603, "switch to this tenant before leaving it")
		return
	}
	if h.writeError(c, h.service.Leave(c.Request.Context(), current.TenantID, current.UserID)) {
		return
	}
	httpx.OK(c, map[string]bool{"success": true})
}

// TransferOwnership 仅允许当前租户所有者转移所有权。
func (h *TenantHandler) TransferOwnership(c *gin.Context) {
	current, _ := requestctx.Get(c)
	if current.TenantID != c.Param("id") || !hasAnyRole(current.Roles, "owner") {
		httpx.Error(c, 403, 14603, "only the owner can transfer ownership")
		return
	}
	var input struct {
		UserID string `json:"userId" binding:"required,uuid"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14605, "invalid ownership transfer input")
		return
	}
	if h.writeError(c, h.service.TransferOwnership(c.Request.Context(), current.TenantID, current.UserID, input.UserID)) {
		return
	}
	httpx.OK(c, map[string]bool{"success": true})
}

// UpdateMemberRole 仅租户所有者可调整成员角色，且不能通过此接口变更所有者。
func (h *TenantHandler) UpdateMemberRole(c *gin.Context) {
	current, _ := requestctx.Get(c)
	if current.TenantID != c.Param("id") || !hasAnyRole(current.Roles, "owner") {
		httpx.Error(c, 403, 14603, "only the owner can change member roles")
		return
	}
	var input UpdateMemberRoleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14611, "invalid member role")
		return
	}
	if h.writeError(c, h.service.UpdateMemberRole(c.Request.Context(), current.TenantID, current.UserID, c.Param("userId"), input.Role)) {
		return
	}
	httpx.OK(c, map[string]string{"role": input.Role})
}

// writeError 将租户领域错误映射为稳定的 HTTP 状态和业务错误码。
func (h *TenantHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrMultiModeOnly):
		httpx.Error(c, 409, 14609, err.Error())
	case errors.Is(err, ErrConflict):
		httpx.Error(c, 409, 14610, err.Error())
	case errors.Is(err, ErrInvalidRole):
		httpx.Error(c, 400, 14612, err.Error())
	case errors.Is(err, ErrTenantNotFound), errors.Is(err, ErrInvitation):
		httpx.Error(c, 404, 14604, err.Error())
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrOwnerMustTransfer):
		httpx.Error(c, 403, 14603, err.Error())
	default:
		httpx.Error(c, 500, 15600, "tenant operation failed")
	}
	return true
}

// hasAnyRole 判断当前身份是否包含任一允许角色。
func hasAnyRole(current []string, expected ...string) bool {
	set := make(map[string]struct{}, len(expected))
	for _, role := range expected {
		set[role] = struct{}{}
	}
	for _, role := range current {
		if _, ok := set[strings.ToLower(role)]; ok {
			return true
		}
	}
	return false
}
