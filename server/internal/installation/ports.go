package installation

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// AccountProvisioner 定义首次安装所需的最小计费账户创建能力。
type AccountProvisioner interface {
	ProvisionAccount(ctx context.Context, tx pgx.Tx, tenantID, subjectType, subjectID string) error
}
