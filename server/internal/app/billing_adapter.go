package app

import (
	"context"
	"errors"

	"gin-dify-saas/server/internal/billing"
	"gin-dify-saas/server/internal/execution"

	"github.com/jackc/pgx/v5"
)

// billingAdapter 将计费模块的公开能力适配为调用方声明的领域端口。
// 当前同库事务由该适配器封装；拆分独立计费服务时，跨库原子性应改为 Outbox 和 Saga。
type billingAdapter struct{ service *billing.BillingService }

// ProvisionAccount 在调用方事务中创建完整计费账户。
func (a billingAdapter) ProvisionAccount(ctx context.Context, tx pgx.Tx, tenantID, subjectType, subjectID string) error {
	_, err := billing.ProvisionAccount(ctx, tx, tenantID, subjectType, subjectID)
	return err
}

// ReserveRun 为智能体运行预占额度，并把计费错误转换为执行领域可识别的错误。
func (a billingAdapter) ReserveRun(ctx context.Context, tx pgx.Tx, tenantID, userID, runID string) error {
	_, err := a.service.Reserve(ctx, tx, tenantID, userID, runID)
	switch {
	case errors.Is(err, billing.ErrAccountNotFound):
		return execution.ErrBillingAccountNotFound
	case errors.Is(err, billing.ErrInsufficientCredits):
		return execution.ErrInsufficientCredits
	default:
		return err
	}
}

// Release 释放尚未结算的运行预占额度。
func (a billingAdapter) Release(ctx context.Context, runID, reason string) error {
	return a.service.Release(ctx, runID, reason)
}

// Settle 按模型实际用量完成运行结算。
func (a billingAdapter) Settle(ctx context.Context, runID string, usage map[string]any) error {
	return a.service.Settle(ctx, runID, usage)
}

// RecordProviderCost 保存本次运行对应的模型供应商成本。
func (a billingAdapter) RecordProviderCost(ctx context.Context, tenantID, runID, usageRecordID string, usage map[string]any) error {
	return a.service.RecordProviderCost(ctx, tenantID, runID, usageRecordID, usage)
}
