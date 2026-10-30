package administration

import (
	"errors"

	"gin-dify-saas/server/internal/transport/httpx"
	"gin-dify-saas/server/internal/transport/requestctx"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// Menus 为已登录用户提供菜单显示配置；接口不包含权限授予信息。
func (h *Handler) Menus(c *gin.Context) {
	items, err := h.service.Menus(c.Request.Context())
	if err != nil {
		httpx.Error(c, 500, 15700, "failed to load menus")
		return
	}
	httpx.OK(c, items)
}

// UpdateMenu 由平台管理员调整菜单标题、顺序或显示状态。
func (h *Handler) UpdateMenu(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input UpdateMenuInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14700, "invalid menu configuration")
		return
	}
	item, err := h.service.UpdateMenu(c.Request.Context(), c.Param("routeName"), current.UserID, input)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(c, 404, 14701, err.Error())
		return
	}
	if errors.Is(err, ErrProtectedMenu) {
		httpx.Error(c, 409, 14702, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15701, "failed to update menu")
		return
	}
	httpx.OK(c, item)
}

// Users 列出平台账号及其超级管理员标记。
func (h *Handler) Users(c *gin.Context) {
	items, err := h.service.Users(c.Request.Context())
	if err != nil {
		httpx.Error(c, 500, 15702, "failed to load users")
		return
	}
	httpx.OK(c, items)
}

// SetPlatformAdmin 授予或撤销安装级管理员权限；最后一位管理员不可撤销。
func (h *Handler) SetPlatformAdmin(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input UpdateAdminInput
	if err := c.ShouldBindJSON(&input); err != nil || input.Enabled == nil {
		httpx.Error(c, 400, 14703, "enabled is required")
		return
	}
	err := h.service.SetPlatformAdmin(c.Request.Context(), c.Param("userId"), current.UserID, *input.Enabled)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(c, 404, 14701, err.Error())
		return
	}
	if errors.Is(err, ErrLastSuperAdmin) {
		httpx.Error(c, 409, 14704, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15703, "failed to update platform role")
		return
	}
	httpx.OK(c, map[string]bool{"enabled": *input.Enabled})
}
