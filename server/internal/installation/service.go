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

type Service struct {
	db             *pgxpool.Pool
	configuredMode string
}

type Status struct {
	Installed       bool   `json:"installed"`
	Mode            string `json:"mode"`
	InstallationID  string `json:"installationId,omitempty"`
	DefaultTenantID string `json:"defaultTenantId,omitempty"`
}

type SetupInput struct {
	PlatformName  string `json:"platformName" binding:"required,min=2,max=100"`
	TenantName    string `json:"tenantName" binding:"required,min=2,max=100"`
	AdminUsername string `json:"adminUsername" binding:"required,min=3,max=64"`
	AdminName     string `json:"adminName" binding:"required,min=2,max=64"`
	AdminPassword string `json:"adminPassword" binding:"required,min=8,max=128"`
}

func NewService(db *pgxpool.Pool, configuredMode string) *Service {
	return &Service{db: db, configuredMode: configuredMode}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
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

func (s *Service) Setup(ctx context.Context, input SetupInput) (Status, error) {
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
	billingAccountID, err := ids.New()
	if err != nil {
		return Status{}, err
	}
	walletID, err := ids.New()
	if err != nil {
		return Status{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return Status{}, fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Status{}, fmt.Errorf("begin installation: %w", err)
	}
	defer tx.Rollback(ctx)

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
		INSERT INTO users (id, username, password_hash, display_name, status)
		VALUES ($1, $2, $3, $4, 'active')
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
		VALUES ($1, $2, $3, $4, 2)
	`, installationID, s.configuredMode, strings.TrimSpace(input.PlatformName), tenantID); err != nil {
		return Status{}, fmt.Errorf("save installation settings: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_accounts (id, tenant_id, subject_type, subject_id, status)
		VALUES ($1, $2, 'tenant', $2, 'active')
	`, billingAccountID, tenantID); err != nil {
		return Status{}, fmt.Errorf("create default billing account: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_accounts (id, billing_account_id) VALUES ($1, $2)
	`, walletID, billingAccountID); err != nil {
		return Status{}, fmt.Errorf("create default wallet: %w", err)
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
