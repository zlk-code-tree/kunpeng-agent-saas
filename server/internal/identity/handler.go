package identity

import (
	"errors"

	"gin-dify-saas/server/internal/platform/httpx"
	"gin-dify-saas/server/internal/platform/requestctx"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

// NewHandler 创建认证与用户信息接口处理器。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Login 处理用户名密码登录请求。
func (h *Handler) Login(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14001, "username and password are required")
		return
	}
	result, err := h.service.Login(c.Request.Context(), input)
	if errors.Is(err, ErrInvalidCredentials) {
		httpx.Error(c, 401, 14012, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15010, "login failed")
		return
	}
	httpx.OK(c, result)
}

// Logout 返回退出成功；当前无状态 JWT 由客户端负责删除。
func (h *Handler) Logout(c *gin.Context) {
	httpx.OK(c, map[string]bool{"success": true})
}

// Codes 返回当前用户的权限码列表供 Vben 前端控制界面访问。
func (h *Handler) Codes(c *gin.Context) {
	identity, ok := requestctx.Get(c)
	if !ok {
		httpx.Error(c, 401, 14010, "missing identity")
		return
	}
	codes := []string{"agent:use", "knowledge:manage"}
	if identity.PlatformAdmin {
		codes = append(codes, "platform:manage")
	}
	httpx.OK(c, codes)
}

// UserInfo 返回当前登录用户资料和角色。
func (h *Handler) UserInfo(c *gin.Context) {
	identity, ok := requestctx.Get(c)
	if !ok {
		httpx.Error(c, 401, 14010, "missing identity")
		return
	}
	info, err := h.service.UserInfo(c.Request.Context(), identity.UserID, identity.Roles)
	if err != nil {
		httpx.Error(c, 500, 15011, "failed to load user")
		return
	}
	httpx.OK(c, info)
}
