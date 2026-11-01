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

// CreateMenu 由平台管理员新增自定义菜单入口（外链或内嵌页面）。
func (h *Handler) CreateMenu(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input CreateMenuInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14707, "invalid menu input")
		return
	}
	item, err := h.service.CreateMenu(c.Request.Context(), current.UserID, input)
	if errors.Is(err, ErrInvalidMenu) {
		httpx.Error(c, 400, 14708, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15705, "failed to create menu")
		return
	}
	httpx.Created(c, item)
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
	if errors.Is(err, ErrInvalidMenu) {
		httpx.Error(c, 400, 14708, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15701, "failed to update menu")
		return
	}
	httpx.OK(c, item)
}

// DeleteMenu 删除自定义菜单；内置路由菜单不可删除。
func (h *Handler) DeleteMenu(c *gin.Context) {
	current, _ := requestctx.Get(c)
	err := h.service.DeleteMenu(c.Request.Context(), c.Param("routeName"), current.UserID)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(c, 404, 14701, err.Error())
		return
	}
	if errors.Is(err, ErrBuiltinMenu) {
		httpx.Error(c, 409, 14709, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15706, "failed to delete menu")
		return
	}
	httpx.OK(c, map[string]bool{"success": true})
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

// CreateUser 创建平台后台账号，可同时授予安装级平台管理员。
func (h *Handler) CreateUser(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input CreateUserInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14705, "invalid user input")
		return
	}
	item, err := h.service.CreateUser(c.Request.Context(), current.UserID, input)
	if errors.Is(err, ErrConflict) {
		httpx.Error(c, 409, 14706, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15704, "failed to create user")
		return
	}
	httpx.Created(c, item)
}

// UpdateUser 修改平台账号姓名、启用状态，并可选重置密码。
func (h *Handler) UpdateUser(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input UpdateUserInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14710, "invalid user input")
		return
	}
	item, err := h.service.UpdateUser(c.Request.Context(), c.Param("userId"), current.UserID, input)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.Error(c, 404, 14701, err.Error())
		return
	case errors.Is(err, ErrSelfDisable), errors.Is(err, ErrLastSuperAdmin):
		httpx.Error(c, 409, 14711, err.Error())
		return
	case err != nil:
		httpx.Error(c, 500, 15707, "failed to update user")
		return
	}
	httpx.OK(c, item)
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
