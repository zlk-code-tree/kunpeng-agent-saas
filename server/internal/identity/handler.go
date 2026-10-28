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

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

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

func (h *Handler) Logout(c *gin.Context) {
	httpx.OK(c, map[string]bool{"success": true})
}

func (h *Handler) Codes(c *gin.Context) {
	identity, ok := requestctx.Get(c)
	if !ok {
		httpx.Error(c, 401, 14010, "missing identity")
		return
	}
	codes := []string{"agent:use", "knowledge:manage"}
	for _, role := range identity.Roles {
		if role == "owner" || role == "admin" {
			codes = append(codes, "platform:manage")
			break
		}
	}
	httpx.OK(c, codes)
}

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
