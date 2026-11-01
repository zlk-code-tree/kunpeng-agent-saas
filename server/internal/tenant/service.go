package tenant

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-dify-saas/server/internal/platform/ids"
	"gin-dify-saas/server/internal/platform/security"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrConflict          = errors.New("resource already exists")
	ErrForbidden         = errors.New("tenant operation is forbidden")
	ErrInvitation        = errors.New("invitation is invalid or expired")
	ErrInvalidRole       = errors.New("invitation role is invalid")
	ErrMultiModeOnly     = errors.New("operation requires multi-tenant mode")
	ErrTenantNotFound    = errors.New("tenant resource not found")
	ErrOwnerMustTransfer = errors.New("tenant owner must transfer ownership before leaving")
)

type TenantService struct {
	db             *pgxpool.Pool
	tokens         *security.TokenManager
	accounts       AccountProvisioner
	configuredMode string
}

// NewTenantService 创建租户、成员和邀请管理服务。
func NewTenantService(db *pgxpool.Pool, tokens *security.TokenManager, accounts AccountProvisioner, configuredMode string) *TenantService {
	return &TenantService{db: db, tokens: tokens, accounts: accounts, configuredMode: configuredMode}
}

// Register 在多租户模式下注册用户，并创建其个人空间、所有者成员关系和计费账户。
func (s *TenantService) Register(ctx context.Context, input RegisterInput) (RegisterResult, error) {
	// 同时检查运行配置和数据库安装模式，防止配置错误的进程在单租户数据库上开放注册。
	if s.configuredMode != "multi" {
		return RegisterResult{}, ErrMultiModeOnly
	}
	var installationID, installedMode string
	if err := s.db.QueryRow(ctx, `SELECT id::text, mode FROM installation_settings LIMIT 1`).Scan(&installationID, &installedMode); err != nil {
		return RegisterResult{}, err
	}
	if installedMode != "multi" {
		return RegisterResult{}, ErrMultiModeOnly
	}

	username := strings.ToLower(strings.TrimSpace(input.Username))
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return RegisterResult{}, err
	}
	userID, _ := ids.New()
	tenantID, _ := ids.New()
	membershipID, _ := ids.New()
	tenantName := strings.TrimSpace(input.DisplayName) + "的个人空间"

	// 用户、个人租户、所有者成员关系和计费账户必须整体创建。
	// SERIALIZABLE 防止并发注册造成同一用户名被部分初始化，或出现没有所有者/钱包的租户。
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return RegisterResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name, status)
		VALUES ($1,$2,$3,$4,'active')
	`, userID, username, string(passwordHash), strings.TrimSpace(input.DisplayName)); err != nil {
		return RegisterResult{}, classifyConflict(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenants (id, type, name, slug, status)
		VALUES ($1,'personal',$2,$3,'active')
	`, tenantID, tenantName, slug("personal", tenantID)); err != nil {
		return RegisterResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_members (id, tenant_id, user_id, role, status)
		VALUES ($1,$2,$3,'owner','active')
	`, membershipID, tenantID, userID); err != nil {
		return RegisterResult{}, err
	}
	if err := syncBuiltinMemberRole(ctx, tx, tenantID, userID, "owner"); err != nil {
		return RegisterResult{}, err
	}
	if err := s.accounts.ProvisionAccount(ctx, tx, tenantID, "user", userID); err != nil {
		return RegisterResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RegisterResult{}, err
	}
	token, err := s.tokens.Issue(userID, username, installationID, tenantID, []string{"owner"})
	if err != nil {
		return RegisterResult{}, err
	}
	return RegisterResult{AccessToken: token, TenantID: tenantID}, nil
}

// List 返回用户加入的所有有效租户，并标记当前令牌所选择的租户。
func (s *TenantService) List(ctx context.Context, userID, currentTenantID string) ([]TenantItem, error) {
	rows, err := s.db.Query(ctx, `
		SELECT t.id::text, t.name, t.type, tm.role, t.status, t.created_at
		FROM tenant_members tm JOIN tenants t ON t.id=tm.tenant_id
		WHERE tm.user_id=$1 AND tm.status='active'
		ORDER BY (t.type='personal') DESC, t.created_at
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TenantItem, 0)
	for rows.Next() {
		var item TenantItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.Role, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Current = item.ID == currentTenantID
		items = append(items, item)
	}
	return items, rows.Err()
}

// Create 在多租户模式下创建企业空间、所有者成员关系和企业共享计费账户。
func (s *TenantService) Create(ctx context.Context, userID string, input TenantCreateInput) (TenantItem, error) {
	if s.configuredMode != "multi" {
		return TenantItem{}, ErrMultiModeOnly
	}
	if err := s.ensureInstalledMulti(ctx); err != nil {
		return TenantItem{}, err
	}
	tenantID, _ := ids.New()
	membershipID, _ := ids.New()
	item := TenantItem{ID: tenantID, Name: strings.TrimSpace(input.Name), Type: "organization", Role: "owner", Status: "active", CreatedAt: time.Now().UTC()}
	// 企业计费归属租户而不是创建者，使企业成员共享额度，同时个人空间仍保留独立用户钱包。
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return TenantItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenants (id, type, name, slug, status) VALUES ($1,'organization',$2,$3,'active')
	`, tenantID, item.Name, slug("org", tenantID)); err != nil {
		return TenantItem{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_members (id, tenant_id, user_id, role, status) VALUES ($1,$2,$3,'owner','active')
	`, membershipID, tenantID, userID); err != nil {
		return TenantItem{}, err
	}
	if err := syncBuiltinMemberRole(ctx, tx, tenantID, userID, "owner"); err != nil {
		return TenantItem{}, err
	}
	if err := s.accounts.ProvisionAccount(ctx, tx, tenantID, "tenant", tenantID); err != nil {
		return TenantItem{}, err
	}
	if err := insertAudit(ctx, tx, tenantID, userID, "tenant.create", "tenant", tenantID, map[string]any{"name": item.Name}); err != nil {
		return TenantItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TenantItem{}, err
	}
	return item, nil
}

// Switch 校验实时成员关系后，为目标租户签发新的访问令牌。
func (s *TenantService) Switch(ctx context.Context, userID, tenantID string) (SwitchResult, error) {
	// 旧令牌不能直接授权租户切换；必须重新读取有效用户、租户和成员角色。
	var installationID, username, role string
	err := s.db.QueryRow(ctx, `
		SELECT i.id::text, u.username, tm.role
		FROM users u
		JOIN tenant_members tm ON tm.user_id=u.id AND tm.tenant_id=$2 AND tm.status='active'
		JOIN tenants t ON t.id=tm.tenant_id AND t.status='active'
		JOIN installation_settings i ON true
		WHERE u.id=$1 AND u.status='active'
	`, userID, tenantID).Scan(&installationID, &username, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return SwitchResult{}, ErrTenantNotFound
	}
	if err != nil {
		return SwitchResult{}, err
	}
	token, err := s.tokens.Issue(userID, username, installationID, tenantID, []string{role})
	if err != nil {
		return SwitchResult{}, err
	}
	return SwitchResult{AccessToken: token, TenantID: tenantID, Role: role}, nil
}

// Members 按角色优先级返回租户当前有效成员。
func (s *TenantService) Members(ctx context.Context, tenantID string) ([]Member, error) {
	rows, err := s.db.Query(ctx, `
		SELECT u.id::text, u.username, u.display_name, tm.role, tm.status, tm.created_at
		FROM tenant_members tm JOIN users u ON u.id=tm.user_id
		WHERE tm.tenant_id=$1 AND tm.status='active'
		ORDER BY CASE tm.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'finance' THEN 2 ELSE 3 END, tm.created_at
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Member, 0)
	for rows.Next() {
		var item Member
		if err := rows.Scan(&item.UserID, &item.Username, &item.DisplayName, &item.Role, &item.Status, &item.JoinedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateMemberRole 由租户所有者调整普通成员角色，立即影响下一次请求的数据库鉴权。
// owner 不在可写角色中，防止绕过 TransferOwnership 的原子所有权转移。
func (s *TenantService) UpdateMemberRole(ctx context.Context, tenantID, actorID, targetID, role string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var actorRole, targetRole string
	if err := tx.QueryRow(ctx, `
		SELECT role FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active'
	`, tenantID, actorID).Scan(&actorRole); err != nil {
		return ErrForbidden
	}
	if actorRole != "owner" {
		return ErrForbidden
	}
	err = tx.QueryRow(ctx, `
		SELECT role FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active' FOR UPDATE
	`, tenantID, targetID).Scan(&targetRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTenantNotFound
	}
	if err != nil {
		return err
	}
	if targetRole == "owner" {
		return ErrOwnerMustTransfer
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tenant_members SET role=$3,updated_at=NOW()
		WHERE tenant_id=$1 AND user_id=$2
	`, tenantID, targetID, role); err != nil {
		return err
	}
	if err := syncBuiltinMemberRole(ctx, tx, tenantID, targetID, role); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (id,tenant_id,actor_user_id,action,resource_type,resource_id,metadata)
		VALUES (gen_random_uuid(),$1,$2,'tenant.member.role.update','tenant_member',$3,
		        jsonb_build_object('oldRole',$4,'newRole',$5))
	`, tenantID, actorID, targetID, targetRole, role); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Invite 为已注册用户创建限时租户邀请，并返回仅展示一次的原始令牌。
func (s *TenantService) Invite(ctx context.Context, tenantID, actorUserID string, input InviteInput) (Invitation, error) {
	var targetUserID, username string
	if err := s.db.QueryRow(ctx, `SELECT id::text, username FROM users WHERE username=$1 AND status='active'`, strings.ToLower(strings.TrimSpace(input.Username))).Scan(&targetUserID, &username); errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrTenantNotFound
	} else if err != nil {
		return Invitation{}, err
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active')`, tenantID, targetUserID).Scan(&exists); err != nil {
		return Invitation{}, err
	}
	if exists {
		return Invitation{}, ErrConflict
	}
	_, _ = s.db.Exec(ctx, `
		UPDATE tenant_invitations SET status='expired', updated_at=NOW()
		WHERE tenant_id=$1 AND invitee_user_id=$2 AND status='pending' AND expires_at <= NOW()
	`, tenantID, targetUserID)
	// 原始令牌只返回一次，数据库仅保存 SHA-256 摘要，数据库泄露后不能直接接受未完成邀请。
	token, tokenHash, err := invitationToken()
	if err != nil {
		return Invitation{}, err
	}
	// 可选的自定义角色必须属于当前租户或内置共享角色（scope 隔离）。
	var roleID any
	if input.RoleID != "" {
		var valid bool
		if err := s.db.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM roles WHERE id=$1 AND scope='tenant' AND (tenant_id IS NULL OR tenant_id=$2))
		`, input.RoleID, tenantID).Scan(&valid); err != nil {
			return Invitation{}, err
		}
		if !valid {
			return Invitation{}, ErrInvalidRole
		}
		roleID = input.RoleID
	}
	id, _ := ids.New()
	expiresAt := time.Now().UTC().Add(72 * time.Hour)
	_, err = s.db.Exec(ctx, `
		INSERT INTO tenant_invitations (id, tenant_id, invitee_user_id, invited_by, role, role_id, token_hash, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, id, tenantID, targetUserID, actorUserID, input.Role, roleID, tokenHash, expiresAt)
	if err != nil {
		return Invitation{}, classifyConflict(err)
	}
	return Invitation{ID: id, TenantID: tenantID, Username: username, Role: input.Role, Token: token, ExpiresAt: expiresAt}, nil
}

// AcceptInvitation 原子消费邀请并创建或恢复用户的租户成员关系。
func (s *TenantService) AcceptInvitation(ctx context.Context, userID, token string) (TenantItem, error) {
	tokenHash := hashToken(strings.TrimSpace(token))
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return TenantItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var invitationID, tenantID, role string
	var extraRoleID *string
	var expiresAt time.Time
	// 锁定待处理邀请，防止并发请求重复接受；成员关系使用 Upsert，支持重新邀请已退出用户。
	err = tx.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, role, role_id::text, expires_at
		FROM tenant_invitations
		WHERE token_hash=$1 AND invitee_user_id=$2 AND status='pending'
		FOR UPDATE
	`, tokenHash, userID).Scan(&invitationID, &tenantID, &role, &extraRoleID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TenantItem{}, ErrInvitation
	}
	if err != nil {
		return TenantItem{}, err
	}
	if time.Now().After(expiresAt) {
		_, _ = tx.Exec(ctx, `UPDATE tenant_invitations SET status='expired', updated_at=NOW() WHERE id=$1`, invitationID)
		return TenantItem{}, ErrInvitation
	}
	membershipID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_members (id, tenant_id, user_id, role, status)
		VALUES ($1,$2,$3,$4,'active')
		ON CONFLICT (tenant_id, user_id) DO UPDATE SET role=EXCLUDED.role, status='active', updated_at=NOW()
	`, membershipID, tenantID, userID, role); err != nil {
		return TenantItem{}, err
	}
	if err := syncBuiltinMemberRole(ctx, tx, tenantID, userID, role); err != nil {
		return TenantItem{}, err
	}
	if extraRoleID != nil && *extraRoleID != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO member_roles (tenant_id, user_id, role_id) VALUES ($1,$2,$3)
			ON CONFLICT DO NOTHING
		`, tenantID, userID, *extraRoleID); err != nil {
			return TenantItem{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE tenant_invitations SET status='accepted', accepted_at=NOW(), updated_at=NOW() WHERE id=$1`, invitationID); err != nil {
		return TenantItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TenantItem{}, err
	}
	var item TenantItem
	if err := s.db.QueryRow(ctx, `SELECT id::text,name,type,$2,status,created_at FROM tenants WHERE id=$1 AND status='active'`, tenantID, role).Scan(&item.ID, &item.Name, &item.Type, &item.Role, &item.Status, &item.CreatedAt); err != nil {
		return TenantItem{}, err
	}
	return item, nil
}

// RemoveMember 按租户角色规则移除目标成员。
func (s *TenantService) RemoveMember(ctx context.Context, tenantID, actorUserID, actorRole, targetUserID string) error {
	if actorUserID == targetUserID {
		return ErrForbidden
	}
	var targetRole string
	if err := s.db.QueryRow(ctx, `SELECT role FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active'`, tenantID, targetUserID).Scan(&targetRole); errors.Is(err, pgx.ErrNoRows) {
		return ErrTenantNotFound
	} else if err != nil {
		return err
	}
	// 所有者必须显式转移所有权，同级管理员不能互相移除，确保高权限变化明确且可追踪。
	if targetRole == "owner" || actorRole == "admin" && targetRole == "admin" {
		return ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `UPDATE tenant_members SET status='removed', updated_at=NOW() WHERE tenant_id=$1 AND user_id=$2 AND status='active'`, tenantID, targetUserID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrTenantNotFound
	}
	// 同时清理角色绑定，防止成员被重新邀请时旧的附加角色残留生效。
	if _, err := tx.Exec(ctx, `DELETE FROM member_roles WHERE tenant_id=$1 AND user_id=$2`, tenantID, targetUserID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Leave 让普通成员退出租户；所有者必须先完成所有权转移。
func (s *TenantService) Leave(ctx context.Context, tenantID, userID string) error {
	var role string
	if err := s.db.QueryRow(ctx, `SELECT role FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active'`, tenantID, userID).Scan(&role); errors.Is(err, pgx.ErrNoRows) {
		return ErrTenantNotFound
	} else if err != nil {
		return err
	}
	if role == "owner" {
		return ErrOwnerMustTransfer
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE tenant_members SET status='left', updated_at=NOW() WHERE tenant_id=$1 AND user_id=$2`, tenantID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM member_roles WHERE tenant_id=$1 AND user_id=$2`, tenantID, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TransferOwnership 原子地把租户所有权从当前所有者转移给目标成员。
func (s *TenantService) TransferOwnership(ctx context.Context, tenantID, ownerUserID, targetUserID string) error {
	if ownerUserID == targetUserID {
		return ErrConflict
	}
	// 原所有者降级和新所有者升级必须原子完成，不能提交双所有者或无所有者的中间状态。
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var targetRole string
	if err := tx.QueryRow(ctx, `SELECT role FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active' FOR UPDATE`, tenantID, targetUserID).Scan(&targetRole); errors.Is(err, pgx.ErrNoRows) {
		return ErrTenantNotFound
	} else if err != nil {
		return err
	}
	if targetRole == "owner" {
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE tenant_members SET role='admin', updated_at=NOW() WHERE tenant_id=$1 AND user_id=$2 AND role='owner'`, tenantID, ownerUserID); err != nil {
		return err
	}
	if err := syncBuiltinMemberRole(ctx, tx, tenantID, ownerUserID, "admin"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE tenant_members SET role='owner', updated_at=NOW() WHERE tenant_id=$1 AND user_id=$2`, tenantID, targetUserID); err != nil {
		return err
	}
	if err := syncBuiltinMemberRole(ctx, tx, tenantID, targetUserID, "owner"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ensureInstalledMulti 校验数据库持久化的安装模式确实为多租户。
func (s *TenantService) ensureInstalledMulti(ctx context.Context) error {
	var mode string
	if err := s.db.QueryRow(ctx, `SELECT mode FROM installation_settings LIMIT 1`).Scan(&mode); err != nil {
		return err
	}
	if mode != "multi" {
		return ErrMultiModeOnly
	}
	return nil
}

// syncBuiltinMemberRole 在调用方事务内把成员的内置角色同步到 member_roles 绑定，
// 保持 tenant_members.role 与 RBAC 角色绑定一致；自定义角色绑定不受影响。
func syncBuiltinMemberRole(ctx context.Context, tx pgx.Tx, tenantID, userID, role string) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM member_roles mr USING roles r
		WHERE mr.tenant_id=$1 AND mr.user_id=$2 AND mr.role_id=r.id AND r.tenant_id IS NULL AND r.scope='tenant'
	`, tenantID, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO member_roles (tenant_id, user_id, role_id)
		SELECT $1, $2, id FROM roles WHERE code=$3 AND scope='tenant' AND tenant_id IS NULL
		ON CONFLICT DO NOTHING
	`, tenantID, userID, role)
	return err
}

// insertAudit 在调用方事务内写入租户操作审计，保证审计与业务变更同时提交。
func insertAudit(ctx context.Context, tx pgx.Tx, tenantID, actorUserID, action, resourceType, resourceID string, metadata map[string]any) error {
	id, _ := ids.New()
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (id, tenant_id, actor_user_id, action, resource_type, resource_id, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, id, tenantID, actorUserID, action, resourceType, resourceID, metadata)
	return err
}

// invitationToken 生成高熵邀请令牌，并同时返回可持久化的摘要。
func invitationToken() (string, string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(value)
	return token, hashToken(token), nil
}

// hashToken 返回令牌的 SHA-256 十六进制摘要。
func hashToken(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

// slug 根据资源类型和 UUID 生成稳定、低冲突的租户短标识。
func slug(prefix, id string) string {
	return fmt.Sprintf("%s-%s", prefix, strings.ReplaceAll(id, "-", "")[:16])
}

// classifyConflict 将 PostgreSQL 唯一约束错误转换为领域冲突错误。
func classifyConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}
