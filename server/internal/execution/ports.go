package execution

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrBillingAccountNotFound 表示当前运行没有可用的计费账户。
	ErrBillingAccountNotFound = errors.New("billing account not found")
	// ErrInsufficientCredits 表示计费系统拒绝了本次额度预占。
	ErrInsufficientCredits = errors.New("insufficient credits")
)

// BillingPort 定义运行领域需要的最小计费能力。
// 当前预占与运行创建共享本地事务；拆分计费微服务时应将该步骤演进为 Outbox 和 Saga，
// 其余结算能力则可直接由 RPC 或消息适配器实现。
type BillingPort interface {
	ReserveRun(ctx context.Context, tx pgx.Tx, tenantID, userID, runID string) error
	Release(ctx context.Context, runID, reason string) error
	Settle(ctx context.Context, runID string, usage map[string]any) error
	RecordProviderCost(ctx context.Context, tenantID, runID, usageRecordID string, usage map[string]any) error
}
