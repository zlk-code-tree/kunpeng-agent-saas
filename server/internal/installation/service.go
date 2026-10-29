package installation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gin-dify-saas/server/internal/platform/ids"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var ErrAlreadyInstalled = errors.New("platform is already installed")

type InstallationService struct {
	db             *pgxpool.Pool
	accounts       AccountProvisioner
	configuredMode string
}

// NewInstallationService 创建平台首次安装服务，并保留进程配置的单/多租户模式。
func NewInstallationService(db *pgxpool.Pool, accounts AccountProvisioner, configuredMode string) *InstallationService {
	return &InstallationService{db: db, accounts: accounts, configuredMode: configuredMode}
}

// Status 返回安装状态，并验证运行配置模式与数据库持久化模式一致。
func (s *InstallationService) Status(ctx context.Context) (Status, error) {
	var status Status
	err := s.db.QueryRow(ctx, `
		SELECT id::text, mode, default_tenant_id::text
		FROM installation_settings
		LIMIT 1
	`).Scan(&status.InstallationID, &status.Mode, &status.DefaultTenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Status{Installed: false, Mode: s.configuredMode}, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("query installation: %w", err)
	}
	if status.Mode != s.configuredMode {
		return Status{}, fmt.Errorf("configured mode %s does not match installed mode %s", s.configuredMode, status.Mode)
	}
	status.Installed = true
	return status, nil
}

// Setup 原子完成首次安装，创建默认租户、平台管理员、计费账户和预置智能体。
func (s *InstallationService) Setup(ctx context.Context, input SetupInput) (Status, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	if status.Installed {
		return Status{}, ErrAlreadyInstalled
	}

	installationID, err := ids.New()
	if err != nil {
		return Status{}, err
	}
	tenantID, err := ids.New()
	if err != nil {
		return Status{}, err
	}
	userID, err := ids.New()
	if err != nil {
		return Status{}, err
	}
	membershipID, err := ids.New()
	if err != nil {
		return Status{}, err
	}
	agentID, err := ids.New()
	if err != nil {
		return Status{}, err
	}
	agentVersionID, err := ids.New()
	if err != nil {
		return Status{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return Status{}, fmt.Errorf("hash password: %w", err)
	}

	// 安装是一次性聚合操作：默认租户、平台管理员、成员关系、计费和预置智能体必须同时存在或同时不存在。
	// SERIALIZABLE 隔离级别也能安全处理多个并发安装请求。
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Status{}, fmt.Errorf("begin installation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 事务外的 Status 只是快速判断，不能阻止并发竞争，因此进入事务后必须再次检查安装锁。
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM installation_settings`).Scan(&count); err != nil {
		return Status{}, fmt.Errorf("check installation lock: %w", err)
	}
	if count > 0 {
		return Status{}, ErrAlreadyInstalled
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenants (id, type, name, slug, status)
		VALUES ($1, 'organization', $2, 'default', 'active')
	`, tenantID, strings.TrimSpace(input.TenantName)); err != nil {
		return Status{}, fmt.Errorf("create default tenant: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name, status, is_platform_admin)
		VALUES ($1, $2, $3, $4, 'active', TRUE)
	`, userID, strings.TrimSpace(input.AdminUsername), string(passwordHash), strings.TrimSpace(input.AdminName)); err != nil {
		return Status{}, fmt.Errorf("create administrator: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_members (id, tenant_id, user_id, role, status)
		VALUES ($1, $2, $3, 'owner', 'active')
	`, membershipID, tenantID, userID); err != nil {
		return Status{}, fmt.Errorf("create administrator membership: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO installation_settings
			(id, mode, platform_name, default_tenant_id, schema_version)
		VALUES ($1, $2, $3, $4, 5)
	`, installationID, s.configuredMode, strings.TrimSpace(input.PlatformName), tenantID); err != nil {
		return Status{}, fmt.Errorf("save installation settings: %w", err)
	}
	if err := s.accounts.ProvisionAccount(ctx, tx, tenantID, "tenant", tenantID); err != nil {
		return Status{}, fmt.Errorf("create default billing account: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_templates (id, name, description, category, status)
		VALUES ($1, '通用知识助手', '结合租户私有知识库进行问答的预置智能体', '通用', 'published')
	`, agentID); err != nil {
		return Status{}, fmt.Errorf("create default agent: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_versions (id, agent_template_id, version, app_type, parameter_schema, status)
		VALUES ($1, $2, 1, 'chatbot', '{"temperature":{"type":"number","minimum":0,"maximum":1}}', 'published')
	`, agentVersionID, agentID); err != nil {
		return Status{}, fmt.Errorf("create default agent version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Status{}, fmt.Errorf("commit installation: %w", err)
	}
	return Status{Installed: true, Mode: s.configuredMode, InstallationID: installationID, DefaultTenantID: tenantID}, nil
}
