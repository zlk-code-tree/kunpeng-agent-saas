package tenant

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// AccountProvisioner 定义租户注册和创建空间所需的最小计费能力。
type AccountProvisioner interface {
	ProvisionAccount(ctx context.Context, tx pgx.Tx, tenantID, subjectType, subjectID string) error
}
