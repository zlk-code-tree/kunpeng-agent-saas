package tenant

import (
	"errors"
	"strings"

	"gin-dify-saas/server/internal/platform/httpx"
	"gin-dify-saas/server/internal/platform/requestctx"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Register(c *gin.Context) {
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

func (h *Handler) List(c *gin.Context) {
	current, _ := requestctx.Get(c)
	items, err := h.service.List(c.Request.Context(), current.UserID, current.TenantID)
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, items)
}

func (h *Handler) Create(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input CreateInput
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

func (h *Handler) Switch(c *gin.Context) {
	current, _ := requestctx.Get(c)
	result, err := h.service.Switch(c.Request.Context(), current.UserID, c.Param("id"))
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, result)
}

func (h *Handler) Members(c *gin.Context) {
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

func (h *Handler) Invite(c *gin.Context) {
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

func (h *Handler) AcceptInvitation(c *gin.Context) {
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

func (h *Handler) RemoveMember(c *gin.Context) {
	current, _ := requestctx.Get(c)
	if current.TenantID != c.Param("id") || !hasAnyRole(current.Roles, "owner", "admin") {
		httpx.Error(c, 403, 14603, "permission denied")
		return
	}
	err := h.service.RemoveMember(c.Request.Context(), current.TenantID, current.UserID, current.Roles[0], c.Param("userId"))
	if h.writeError(c, err) {
		return
	}
	httpx.OK(c, map[string]bool{"success": true})
}

func (h *Handler) Leave(c *gin.Context) {
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

func (h *Handler) TransferOwnership(c *gin.Context) {
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

func (h *Handler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrMultiModeOnly):
		httpx.Error(c, 409, 14609, err.Error())
	case errors.Is(err, ErrConflict):
		httpx.Error(c, 409, 14610, err.Error())
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrInvitation):
		httpx.Error(c, 404, 14604, err.Error())
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrOwnerMustTransfer):
		httpx.Error(c, 403, 14603, err.Error())
	default:
		httpx.Error(c, 500, 15600, "tenant operation failed")
	}
	return true
}

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
