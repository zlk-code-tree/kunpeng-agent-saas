package middleware

import (
	"errors"
	"strings"

	"gin-dify-saas/server/internal/platform/security"
	"gin-dify-saas/server/internal/transport/httpx"
	"gin-dify-saas/server/internal/transport/requestctx"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Middleware 验证访问令牌，并从数据库重新加载当前租户成员关系和平台权限。
func Auth(db *pgxpool.Pool, tokens *security.TokenManager) gin.HandlerFunc {
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
		// JWT 只负责选择安装、用户和租户，不作为实时授权来源。每次请求重新校验成员关系，
		// 使角色变更、成员移除和租户停用立即生效。
		var role string
		var platformAdmin bool
		err = db.QueryRow(c.Request.Context(), `
			SELECT tm.role,u.is_platform_admin
			FROM users u
			JOIN tenant_members tm ON tm.user_id=u.id AND tm.tenant_id=$2 AND tm.status='active'
			JOIN tenants t ON t.id=tm.tenant_id AND t.status='active'
			JOIN installation_settings i ON i.id=$3
			WHERE u.id=$1 AND u.status='active'
		`, claims.Subject, claims.TenantID, claims.InstallationID).Scan(&role, &platformAdmin)
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Error(c, 401, 14013, "user or tenant membership is inactive")
			return
		}
		if err != nil {
			httpx.Error(c, 503, 15012, "identity validation is unavailable")
			return
		}
		// 平台管理员是安装级权限，与用户在当前租户内的角色相互独立。
		roles := []string{role}
		if platformAdmin {
			roles = append(roles, "platform_admin")
		}
		requestctx.Set(c, requestctx.Identity{
			InstallationID: claims.InstallationID,
			TenantID:       claims.TenantID,
			UserID:         claims.Subject,
			Username:       claims.Username,
			Roles:          roles,
			PlatformAdmin:  platformAdmin,
		})
		c.Next()
	}
}

// RequirePlatformAdmin 限制接口只能由安装级平台管理员访问。
func RequirePlatformAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		current, ok := requestctx.Get(c)
		if !ok {
			httpx.Error(c, 401, 14010, "missing identity")
			return
		}
		if !current.PlatformAdmin {
			httpx.Error(c, 403, 14030, "platform administrator permission required")
			return
		}
		c.Next()
	}
}

// RequireRoles 限制接口只能由具备任一指定角色的当前身份访问。
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
