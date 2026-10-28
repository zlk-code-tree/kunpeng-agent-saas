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

func NewService(db *pgxpool.Pool, tokens *identity.TokenManager, configuredMode string) *Service {
	return &Service{db: db, tokens: tokens, configuredMode: configuredMode}
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (RegisterResult, error) {
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
	billingAccountID, _ := ids.New()
	walletID, _ := ids.New()
	tenantName := strings.TrimSpace(input.DisplayName) + "的个人空间"

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
	if err := createBillingAccount(ctx, tx, billingAccountID, walletID, tenantID, "user", userID); err != nil {
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

func (s *Service) Create(ctx context.Context, userID string, input CreateInput) (Item, error) {
	if s.configuredMode != "multi" {
		return Item{}, ErrMultiModeOnly
	}
	if err := s.ensureInstalledMulti(ctx); err != nil {
		return Item{}, err
	}
	tenantID, _ := ids.New()
	membershipID, _ := ids.New()
	billingAccountID, _ := ids.New()
	walletID, _ := ids.New()
	item := Item{ID: tenantID, Name: strings.TrimSpace(input.Name), Type: "organization", Role: "owner", Status: "active", CreatedAt: time.Now().UTC()}
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
	if err := createBillingAccount(ctx, tx, billingAccountID, walletID, tenantID, "tenant", tenantID); err != nil {
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

func (s *Service) Switch(ctx context.Context, userID, tenantID string) (SwitchResult, error) {
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

func (s *Service) AcceptInvitation(ctx context.Context, userID, token string) (Item, error) {
	tokenHash := hashToken(strings.TrimSpace(token))
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback(ctx)
	var invitationID, tenantID, role string
	var expiresAt time.Time
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

func (s *Service) TransferOwnership(ctx context.Context, tenantID, ownerUserID, targetUserID string) error {
	if ownerUserID == targetUserID {
		return ErrConflict
	}
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

func createBillingAccount(ctx context.Context, tx pgx.Tx, accountID, walletID, tenantID, subjectType, subjectID string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_accounts (id, tenant_id, subject_type, subject_id, status)
		VALUES ($1,$2,$3,$4,'active')
	`, accountID, tenantID, subjectType, subjectID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO wallet_accounts (id, billing_account_id) VALUES ($1,$2)`, walletID, accountID)
	return err
}

func insertAudit(ctx context.Context, tx pgx.Tx, tenantID, actorUserID, action, resourceType, resourceID string, metadata map[string]any) error {
	id, _ := ids.New()
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (id, tenant_id, actor_user_id, action, resource_type, resource_id, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, id, tenantID, actorUserID, action, resourceType, resourceID, metadata)
	return err
}

func invitationToken() (string, string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(value)
	return token, hashToken(token), nil
}

func hashToken(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func slug(prefix, id string) string {
	return fmt.Sprintf("%s-%s", prefix, strings.ReplaceAll(id, "-", "")[:16])
}

func classifyConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}
