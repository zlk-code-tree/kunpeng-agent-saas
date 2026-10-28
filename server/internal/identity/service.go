package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

type Service struct {
	db     *pgxpool.Pool
	tokens *TokenManager
}

type LoginInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResult struct {
	AccessToken string `json:"accessToken"`
}

type UserInfo struct {
	UserID   string   `json:"userId"`
	Username string   `json:"username"`
	RealName string   `json:"realName"`
	Avatar   string   `json:"avatar"`
	Roles    []string `json:"roles"`
	Desc     string   `json:"desc"`
	HomePath string   `json:"homePath"`
	Token    string   `json:"token"`
}

func NewService(db *pgxpool.Pool, tokens *TokenManager) *Service {
	return &Service{db: db, tokens: tokens}
}

func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	var userID, username, passwordHash, displayName, installationID, tenantID, role string
	err := s.db.QueryRow(ctx, `
		SELECT u.id::text, u.username, u.password_hash, u.display_name,
		       i.id::text, tm.tenant_id::text, tm.role
		FROM users u
		JOIN tenant_members tm ON tm.user_id = u.id AND tm.status = 'active'
		JOIN installation_settings i ON i.default_tenant_id = tm.tenant_id OR i.mode = 'multi'
		WHERE u.username = $1 AND u.status = 'active'
		ORDER BY (tm.tenant_id = i.default_tenant_id) DESC, tm.created_at ASC
		LIMIT 1
	`, input.Username).Scan(&userID, &username, &passwordHash, &displayName, &installationID, &tenantID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("query user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.Password)) != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	token, err := s.tokens.Issue(userID, username, installationID, tenantID, []string{role})
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue access token: %w", err)
	}
	return LoginResult{AccessToken: token}, nil
}

func (s *Service) UserInfo(ctx context.Context, userID string, roles []string) (UserInfo, error) {
	var info UserInfo
	err := s.db.QueryRow(ctx, `
		SELECT id::text, username, display_name, COALESCE(avatar_url, '')
		FROM users WHERE id = $1 AND status = 'active'
	`, userID).Scan(&info.UserID, &info.Username, &info.RealName, &info.Avatar)
	if err != nil {
		return UserInfo{}, fmt.Errorf("query user info: %w", err)
	}
	info.Roles = roles
	info.Desc = "Gin Dify SaaS user"
	info.HomePath = "/workspace"
	return info, nil
}
