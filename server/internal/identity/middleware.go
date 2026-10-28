package identity

import (
	"errors"
	"strings"

	"gin-dify-saas/server/internal/platform/httpx"
	"gin-dify-saas/server/internal/platform/requestctx"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Middleware(db *pgxpool.Pool, tokens *TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			httpx.Error(c, 401, 14010, "missing access token")
			return
		}
		claims, err := tokens.Parse(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			httpx.Error(c, 401, 14011, "invalid or expired access token")
			return
		}
		var role string
		err = db.QueryRow(c.Request.Context(), `
			SELECT tm.role
			FROM users u
			JOIN tenant_members tm ON tm.user_id=u.id AND tm.tenant_id=$2 AND tm.status='active'
			JOIN tenants t ON t.id=tm.tenant_id AND t.status='active'
			JOIN installation_settings i ON i.id=$3
			WHERE u.id=$1 AND u.status='active'
		`, claims.Subject, claims.TenantID, claims.InstallationID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Error(c, 401, 14013, "user or tenant membership is inactive")
			return
		}
		if err != nil {
			httpx.Error(c, 503, 15012, "identity validation is unavailable")
			return
		}
		requestctx.Set(c, requestctx.Identity{
			InstallationID: claims.InstallationID,
			TenantID:       claims.TenantID,
			UserID:         claims.Subject,
			Username:       claims.Username,
			Roles:          []string{role},
		})
		c.Next()
	}
}

func RequireRoles(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(c *gin.Context) {
		current, ok := requestctx.Get(c)
		if !ok {
			httpx.Error(c, 401, 14010, "missing identity")
			return
		}
		for _, role := range current.Roles {
			if _, ok := allowed[role]; ok {
				c.Next()
				return
			}
		}
		httpx.Error(c, 403, 14030, "permission denied")
	}
}
