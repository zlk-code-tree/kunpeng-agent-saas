package identity

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	InstallationID string   `json:"installation_id"`
	TenantID       string   `json:"tenant_id"`
	Username       string   `json:"username"`
	Roles          []string `json:"roles"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenManager 创建使用 HS256 和固定有效期的访问令牌管理器。
func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

// Issue 为指定用户和当前租户签发访问令牌。
func (m *TokenManager) Issue(userID, username, installationID, tenantID string, roles []string) (string, error) {
	now := time.Now()
	claims := Claims{
		InstallationID: installationID,
		TenantID:       tenantID,
		Username:       username,
		Roles:          roles,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: userID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// Parse 校验签名算法、签名和有效期，并返回令牌声明。
func (m *TokenManager) Parse(value string) (Claims, error) {
	token, err := jwt.ParseWithClaims(value, &Claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	})
	if err != nil || !token.Valid {
		return Claims{}, errors.New("invalid access token")
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return Claims{}, errors.New("invalid access token claims")
	}
	return *claims, nil
}
