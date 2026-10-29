package billing

import (
	"errors"
	"strconv"
	"time"

	"gin-dify-saas/server/internal/transport/httpx"
	"gin-dify-saas/server/internal/transport/requestctx"

	"github.com/gin-gonic/gin"
)

type BillingHandler struct{ service *BillingService }

// NewBillingHandler 创建计费接口处理器。
func NewBillingHandler(service *BillingService) *BillingHandler {
	return &BillingHandler{service: service}
}

// CurrentAccount 返回当前租户对应计费主体的钱包余额。
func (h *BillingHandler) CurrentAccount(c *gin.Context) {
	current, _ := requestctx.Get(c)
	item, err := h.service.CurrentAccount(c.Request.Context(), current.TenantID, current.UserID)
	if errors.Is(err, ErrAccountNotFound) {
		httpx.Error(c, 404, 14704, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15700, "failed to load billing account")
		return
	}
	httpx.OK(c, item)
}

// CurrentSubscription 返回当前有效订阅和权益使用量。
func (h *BillingHandler) CurrentSubscription(c *gin.Context) {
	current, _ := requestctx.Get(c)
	item, err := h.service.CurrentSubscription(c.Request.Context(), current.TenantID, current.UserID)
	if errors.Is(err, ErrAccountNotFound) {
		httpx.Error(c, 404, 14704, "active subscription not found")
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15703, "failed to load subscription")
		return
	}
	httpx.OK(c, item)
}

// Ledger 返回当前计费主体最近的钱包流水。
func (h *BillingHandler) Ledger(c *gin.Context) {
	current, _ := requestctx.Get(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.service.ListLedger(c.Request.Context(), current.TenantID, current.UserID, limit)
	if err != nil {
		httpx.Error(c, 500, 15704, "failed to load wallet ledger")
		return
	}
	httpx.OK(c, items)
}

// Plans 返回当前租户可购买或使用的套餐版本。
func (h *BillingHandler) Plans(c *gin.Context) {
	current, _ := requestctx.Get(c)
	items, err := h.service.ListPlans(c.Request.Context(), current.TenantID)
	if err != nil {
		httpx.Error(c, 500, 15701, "failed to load plans")
		return
	}
	httpx.OK(c, items)
}

// Adjust 处理平台管理员人工调账请求。
func (h *BillingHandler) Adjust(c *gin.Context) {
	current, _ := requestctx.Get(c)
	var input AdjustmentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14700, "invalid adjustment input")
		return
	}
	item, err := h.service.Adjust(c.Request.Context(), current.TenantID, c.Param("id"), current.UserID, input)
	if errors.Is(err, ErrInvalidAmount) {
		httpx.Error(c, 409, 14701, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15702, "wallet adjustment failed")
		return
	}
	httpx.OK(c, item)
}

// CostSummary 返回指定时间区间的上游供应商成本汇总。
func (h *BillingHandler) CostSummary(c *gin.Context) {
	current, _ := requestctx.Get(c)
	to := time.Now().UTC()
	from := to.AddDate(0, -1, 0)
	if value := c.Query("from"); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			from = parsed
		}
	}
	if value := c.Query("to"); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			to = parsed
		}
	}
	items, err := h.service.CostSummary(c.Request.Context(), current.TenantID, from, to)
	if err != nil {
		httpx.Error(c, 500, 15710, "failed to load cost summary")
		return
	}
	httpx.OK(c, items)
}

// Reconcile 执行一次只读账务对账扫描。
func (h *BillingHandler) Reconcile(c *gin.Context) {
	current, _ := requestctx.Get(c)
	report, err := h.service.Reconcile(c.Request.Context(), current.TenantID)
	if err != nil {
		httpx.Error(c, 500, 15711, "billing reconciliation failed")
		return
	}
	httpx.OK(c, report)
}
