package execution

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"gin-dify-saas/server/internal/platform/httpx"
	"gin-dify-saas/server/internal/platform/requestctx"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Create(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	var input CreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14400, "invalid run input")
		return
	}
	item, err := h.service.Create(c.Request.Context(), identity.TenantID, identity.UserID, c.GetHeader("Idempotency-Key"), input)
	if err != nil {
		httpx.Error(c, 500, 15400, err.Error())
		return
	}
	httpx.Created(c, item)
}

func (h *Handler) Get(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	item, err := h.service.Get(c.Request.Context(), identity.TenantID, identity.UserID, c.Param("id"))
	if errors.Is(err, ErrNotFound) {
		httpx.Error(c, 404, 14404, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15401, "failed to load run")
		return
	}
	httpx.OK(c, item)
}

func (h *Handler) Cancel(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	err := h.service.Cancel(c.Request.Context(), identity.TenantID, identity.UserID, c.Param("id"))
	if errors.Is(err, ErrNotFound) {
		httpx.Error(c, 404, 14404, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15402, "failed to cancel run")
		return
	}
	httpx.OK(c, map[string]bool{"success": true})
}

func (h *Handler) Events(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	after, _ := strconv.ParseInt(c.GetHeader("Last-Event-ID"), 10, 64)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		httpx.Error(c, 500, 15403, "streaming is unavailable")
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, err := h.service.Events(c.Request.Context(), identity.TenantID, identity.UserID, c.Param("id"), after)
		if errors.Is(err, ErrNotFound) {
			return
		}
		if err != nil {
			return
		}
		for _, event := range events {
			payload, _ := json.Marshal(event)
			fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.EventType, payload)
			after = event.Sequence
			flusher.Flush()
			if event.EventType == "run.succeeded" || event.EventType == "run.failed" || event.EventType == "run.stopped" {
				return
			}
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
