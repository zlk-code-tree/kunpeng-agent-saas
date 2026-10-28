package execution

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"gin-dify-saas/server/internal/billing"
	"gin-dify-saas/server/internal/platform/httpx"
	"gin-dify-saas/server/internal/platform/requestctx"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

// NewHandler 创建智能体运行接口处理器。
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// Create 校验请求并创建一条异步智能体运行任务。
func (h *Handler) Create(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	var input CreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14400, "invalid run input")
		return
	}
	item, err := h.service.Create(c.Request.Context(), identity.TenantID, identity.UserID, c.GetHeader("Idempotency-Key"), input)
	if errors.Is(err, billing.ErrInsufficientCredits) {
		httpx.Error(c, 402, 14402, err.Error())
		return
	}
	if errors.Is(err, billing.ErrAccountNotFound) {
		httpx.Error(c, 409, 14403, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15400, err.Error())
		return
	}
	httpx.Created(c, item)
}

// Get 返回当前用户的一次运行详情。
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

// Cancel 提交运行取消请求，最终停止和计费由 Worker 完成。
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

// Events 通过 SSE 持续推送运行事件，直到任务进入终态或客户端断开连接。
func (h *Handler) Events(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	// Last-Event-ID 支持断线续传，避免重复发送客户端已经渲染的增量。
	// 事件分批读取并逐条刷新，使代理和浏览器能在任务运行期间实时展示内容。
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
		// 轮询共享数据库而不是依赖进程内发布订阅，使任意 API 副本都可以推送任意 Worker 产生的事件。
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
