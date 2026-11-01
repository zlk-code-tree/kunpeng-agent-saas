package rbac

import (
	"errors"

	"gin-dify-saas/server/internal/transport/httpx"
	"gin-dify-saas/server/internal/transport/requestctx"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// Permissions 返回权限码目录；任何已登录成员可读，用于角色勾选面板展示。
func (h *Handler) Permissions(c *gin.Context) {
	items, err := h.service.Permissions(c.Request.Context())
	if err != nil {
		httpx.Error(c, 500, 15800, "failed to load permissions")
		return
	}
	httpx.OK(c, items)
}

// Roles 返回当前租户的内置与自定义角色。
func (h *Handler) Roles(c *gin.Context) {
	current, _ := requestctx.Get(c)
	items, err := h.service.Roles(c.Request.Context(), current.TenantID)
	if err != nil {
		httpx.Error(c, 500, 15801, "failed to load roles")
		return
	}
	httpx.OK(c, items)
}

// CreateRole 新建当前租户的自定义角色。
func (h *Handler) CreateRole(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input CreateRoleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14800, "invalid role input")
		return
	}
	item, err := h.service.CreateRole(c.Request.Context(), current.TenantID, current.UserID, input)
	if h.writeError(c, err) {
		return
	}
	httpx.Created(c, item)
}

// UpdateRole 更新当前租户的自定义角色。
func (h *Handler) UpdateRole(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input UpdateRoleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14800, "invalid role input")
		return
	}
	item, err := h.service.UpdateRole(c.Request.Context(), current.TenantID, c.Param("roleId"), current.UserID, input)
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, item)
}

// DeleteRole 删除当前租户的自定义角色。
func (h *Handler) DeleteRole(c *gin.Context) {
	current, _ := requestctx.Get(c)
	if err := h.service.DeleteRole(c.Request.Context(), current.TenantID, c.Param("roleId"), current.UserID); h.writeError(c, err) {
		return
	}
	httpx.OK(c, map[string]bool{"success": true})
}

// SetMemberRoles 为当前租户成员设置自定义角色集合。
func (h *Handler) SetMemberRoles(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input SetMemberRolesInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14800, "invalid member roles input")
		return
	}
	if input.RoleIDs == nil {
		input.RoleIDs = []string{}
	}
	err := h.service.SetMemberRoles(c.Request.Context(), current.TenantID, current.UserID, c.Param("userId"), input.RoleIDs)
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, map[string]bool{"success": true})
}

// MemberRoles 返回成员已分配的自定义角色 ID 列表。
func (h *Handler) MemberRoles(c *gin.Context) {
	current, _ := requestctx.Get(c)
	roleIDs, err := h.service.MemberCustomRoleIDs(c.Request.Context(), current.TenantID, c.Param("userId"))
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, map[string][]string{"roleIds": roleIDs})
}

// writeError 将 RBAC 领域错误映射为稳定的 HTTP 状态和业务错误码。
func (h *Handler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrMemberNotFound):
		httpx.Error(c, 404, 14801, err.Error())
	case errors.Is(err, ErrBuiltinRole), errors.Is(err, ErrScopeViolation), errors.Is(err, ErrInvalidRoleRef):
		httpx.Error(c, 409, 14802, err.Error())
	case errors.Is(err, ErrConflict):
		httpx.Error(c, 409, 14803, err.Error())
	default:
		httpx.Error(c, 500, 15802, "role operation failed")
	}
	return true
}
