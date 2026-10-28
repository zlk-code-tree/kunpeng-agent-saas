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

	"gin-dify-saas/server/internal/billing"
	"gin-dify-saas/server/internal/identity"
	"gin-dify-saas/server/internal/platform/ids"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrConflict          = errors.New("resource already exists")
	ErrForbidden         = errors.New("tenant operation is forbidden")
	ErrInvitation        = errors.New("invitation is invalid or expired")
	ErrMultiModeOnly     = errors.New("operation requires multi-tenant mode")
	ErrNotFound          = errors.New("tenant resource not found")
	ErrOwnerMustTransfer = errors.New("tenant owner must transfer ownership before leaving")
)

type Service struct {
	db             *pgxpool.Pool
	tokens         *identity.TokenManager
	configuredMode string
}

type Item struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	Current   bool      `json:"current"`
	CreatedAt time.Time `json:"createdAt"`
}

type Member struct {
	UserID      string    `json:"userId"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	JoinedAt    time.Time `json:"joinedAt"`
}

type RegisterInput struct {
	Username    string `json:"username" binding:"required,min=3,max=64"`
	DisplayName string `json:"displayName" binding:"required,min=2,max=64"`
	Password    string `json:"password" binding:"required,min=8,max=128"`
}

type RegisterResult struct {
	AccessToken string `json:"accessToken"`
	TenantID    string `json:"tenantId"`
}

type CreateInput struct {
	Name string `json:"name" binding:"required,min=2,max=100"`
}

type SwitchResult struct {
	AccessToken string `json:"accessToken"`
	TenantID    string `json:"tenantId"`
	Role        string `json:"role"`
}

type InviteInput struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Role     string `json:"role" binding:"required,oneof=admin member finance"`
}

type Invitation struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// NewService 创建租户、成员和邀请管理服务。
func NewService(db *pgxpool.Pool, tokens *identity.TokenManager, configuredMode string) *Service {
	return &Service{db: db, tokens: tokens, configuredMode: configuredMode}
}

// Register 在多租户模式下注册用户，并创建其个人空间、所有者成员关系和计费账户。
func (s *Service) Register(ctx context.Context, input RegisterInput) (RegisterResult, error) {
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
	defer tx.Rollback(ctx)
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
	if _, err := billing.ProvisionAccount(ctx, tx, tenantID, "user", userID); err != nil {
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
func (s *Service) List(ctx context.Context, userID, currentTenantID string) ([]Item, error) {
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
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.Role, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Current = item.ID == currentTenantID
		items = append(items, item)
	}
	return items, rows.Err()
}

// Create 在多租户模式下创建企业空间、所有者成员关系和企业共享计费账户。
func (s *Service) Create(ctx context.Context, userID string, input CreateInput) (Item, error) {
	if s.configuredMode != "multi" {
		return Item{}, ErrMultiModeOnly
	}
	if err := s.ensureInstalledMulti(ctx); err != nil {
		return Item{}, err
	}
	tenantID, _ := ids.New()
	membershipID, _ := ids.New()
	item := Item{ID: tenantID, Name: strings.TrimSpace(input.Name), Type: "organization", Role: "owner", Status: "active", CreatedAt: time.Now().UTC()}
	// 企业计费归属租户而不是创建者，使企业成员共享额度，同时个人空间仍保留独立用户钱包。
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenants (id, type, name, slug, status) VALUES ($1,'organization',$2,$3,'active')
	`, tenantID, item.Name, slug("org", tenantID)); err != nil {
		return Item{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_members (id, tenant_id, user_id, role, status) VALUES ($1,$2,$3,'owner','active')
	`, membershipID, tenantID, userID); err != nil {
		return Item{}, err
	}
	if _, err := billing.ProvisionAccount(ctx, tx, tenantID, "tenant", tenantID); err != nil {
		return Item{}, err
	}
	if err := insertAudit(ctx, tx, tenantID, userID, "tenant.create", "tenant", tenantID, map[string]any{"name": item.Name}); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, err
	}
	return item, nil
}

// Switch 校验实时成员关系后，为目标租户签发新的访问令牌。
func (s *Service) Switch(ctx context.Context, userID, tenantID string) (SwitchResult, error) {
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
		return SwitchResult{}, ErrNotFound
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
func (s *Service) Members(ctx context.Context, tenantID string) ([]Member, error) {
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

// Invite 为已注册用户创建限时租户邀请，并返回仅展示一次的原始令牌。
func (s *Service) Invite(ctx context.Context, tenantID, actorUserID string, input InviteInput) (Invitation, error) {
	var targetUserID, username string
	if err := s.db.QueryRow(ctx, `SELECT id::text, username FROM users WHERE username=$1 AND status='active'`, strings.ToLower(strings.TrimSpace(input.Username))).Scan(&targetUserID, &username); errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrNotFound
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
	id, _ := ids.New()
	expiresAt := time.Now().UTC().Add(72 * time.Hour)
	_, err = s.db.Exec(ctx, `
		INSERT INTO tenant_invitations (id, tenant_id, invitee_user_id, invited_by, role, token_hash, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, id, tenantID, targetUserID, actorUserID, input.Role, tokenHash, expiresAt)
	if err != nil {
		return Invitation{}, classifyConflict(err)
	}
	return Invitation{ID: id, TenantID: tenantID, Username: username, Role: input.Role, Token: token, ExpiresAt: expiresAt}, nil
}

// AcceptInvitation 原子消费邀请并创建或恢复用户的租户成员关系。
func (s *Service) AcceptInvitation(ctx context.Context, userID, token string) (Item, error) {
	tokenHash := hashToken(strings.TrimSpace(token))
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback(ctx)
	var invitationID, tenantID, role string
	var expiresAt time.Time
	// 锁定待处理邀请，防止并发请求重复接受；成员关系使用 Upsert，支持重新邀请已退出用户。
	err = tx.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, role, expires_at
		FROM tenant_invitations
		WHERE token_hash=$1 AND invitee_user_id=$2 AND status='pending'
		FOR UPDATE
	`, tokenHash, userID).Scan(&invitationID, &tenantID, &role, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrInvitation
	}
	if err != nil {
		return Item{}, err
	}
	if time.Now().After(expiresAt) {
		_, _ = tx.Exec(ctx, `UPDATE tenant_invitations SET status='expired', updated_at=NOW() WHERE id=$1`, invitationID)
		return Item{}, ErrInvitation
	}
	membershipID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_members (id, tenant_id, user_id, role, status)
		VALUES ($1,$2,$3,$4,'active')
		ON CONFLICT (tenant_id, user_id) DO UPDATE SET role=EXCLUDED.role, status='active', updated_at=NOW()
	`, membershipID, tenantID, userID, role); err != nil {
		return Item{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE tenant_invitations SET status='accepted', accepted_at=NOW(), updated_at=NOW() WHERE id=$1`, invitationID); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, err
	}
	var item Item
	if err := s.db.QueryRow(ctx, `SELECT id::text,name,type,$2,status,created_at FROM tenants WHERE id=$1 AND status='active'`, tenantID, role).Scan(&item.ID, &item.Name, &item.Type, &item.Role, &item.Status, &item.CreatedAt); err != nil {
		return Item{}, err
	}
	return item, nil
}

// RemoveMember 按租户角色规则移除目标成员。
func (s *Service) RemoveMember(ctx context.Context, tenantID, actorUserID, actorRole, targetUserID string) error {
	if actorUserID == targetUserID {
		return ErrForbidden
	}
	var targetRole string
	if err := s.db.QueryRow(ctx, `SELECT role FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active'`, tenantID, targetUserID).Scan(&targetRole); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	// 所有者必须显式转移所有权，同级管理员不能互相移除，确保高权限变化明确且可追踪。
	if targetRole == "owner" || actorRole == "admin" && targetRole == "admin" {
		return ErrForbidden
	}
	result, err := s.db.Exec(ctx, `UPDATE tenant_members SET status='removed', updated_at=NOW() WHERE tenant_id=$1 AND user_id=$2 AND status='active'`, tenantID, targetUserID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Leave 让普通成员退出租户；所有者必须先完成所有权转移。
func (s *Service) Leave(ctx context.Context, tenantID, userID string) error {
	var role string
	if err := s.db.QueryRow(ctx, `SELECT role FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active'`, tenantID, userID).Scan(&role); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if role == "owner" {
		return ErrOwnerMustTransfer
	}
	_, err := s.db.Exec(ctx, `UPDATE tenant_members SET status='left', updated_at=NOW() WHERE tenant_id=$1 AND user_id=$2`, tenantID, userID)
	return err
}

// TransferOwnership 原子地把租户所有权从当前所有者转移给目标成员。
func (s *Service) TransferOwnership(ctx context.Context, tenantID, ownerUserID, targetUserID string) error {
	if ownerUserID == targetUserID {
		return ErrConflict
	}
	// 原所有者降级和新所有者升级必须原子完成，不能提交双所有者或无所有者的中间状态。
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var targetRole string
	if err := tx.QueryRow(ctx, `SELECT role FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active' FOR UPDATE`, tenantID, targetUserID).Scan(&targetRole); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if targetRole == "owner" {
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE tenant_members SET role='admin', updated_at=NOW() WHERE tenant_id=$1 AND user_id=$2 AND role='owner'`, tenantID, ownerUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE tenant_members SET role='owner', updated_at=NOW() WHERE tenant_id=$1 AND user_id=$2`, tenantID, targetUserID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ensureInstalledMulti 校验数据库持久化的安装模式确实为多租户。
func (s *Service) ensureInstalledMulti(ctx context.Context) error {
	var mode string
	if err := s.db.QueryRow(ctx, `SELECT mode FROM installation_settings LIMIT 1`).Scan(&mode); err != nil {
		return err
	}
	if mode != "multi" {
		return ErrMultiModeOnly
	}
	return nil
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
