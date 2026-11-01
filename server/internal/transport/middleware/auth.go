package middleware

import (
	"context"
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
		// 实时计算权限码：内置角色 + 成员附加角色 + owner/平台管理员旁路。
		// 角色或授权变更在下一次请求立即生效，与成员校验保持一致的实时性。
		codes, err := loadPermissionCodes(c.Request.Context(), db, claims.Subject, claims.TenantID, role, platformAdmin)
		if err != nil {
			httpx.Error(c, 503, 15012, "identity validation is unavailable")
			return
		}
		requestctx.Set(c, requestctx.Identity{
			InstallationID:  claims.InstallationID,
			TenantID:        claims.TenantID,
			UserID:          claims.Subject,
			Username:        claims.Username,
			Roles:           roles,
			PlatformAdmin:   platformAdmin,
			PermissionCodes: codes,
		})
		c.Next()
	}
}

// loadPermissionCodes 汇总当前身份的有效权限码：
// 租户码 = 内置角色(tenant_members.role) + member_roles 附加角色的码，owner 旁路全部租户码；
// 平台码 = user_roles 平台角色的码，平台管理员旁路全部平台码。
func loadPermissionCodes(ctx context.Context, db *pgxpool.Pool, userID, tenantID, role string, platformAdmin bool) ([]string, error) {
	rows, err := db.Query(ctx, `
		SELECT DISTINCT p.code
		FROM permissions p
		LEFT JOIN role_permissions rp ON rp.permission_code = p.code
		LEFT JOIN roles r ON r.id = rp.role_id
		WHERE (p.scope = 'tenant' AND ($4 = 'owner'
		        OR (r.id IS NOT NULL AND (r.code = $3
		            OR r.id IN (SELECT mr.role_id FROM member_roles mr
		                        JOIN tenant_members mtm ON mtm.tenant_id = mr.tenant_id
		                             AND mtm.user_id = mr.user_id AND mtm.status = 'active'
		                        WHERE mr.tenant_id = $2 AND mr.user_id = $1)))))
		   OR (p.scope = 'platform' AND ($5
		        OR (r.id IS NOT NULL AND r.id IN (SELECT ur.role_id FROM user_roles ur WHERE ur.user_id = $1))))
		ORDER BY 1
	`, userID, tenantID, role, role, platformAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	codes := make([]string, 0)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, rows.Err()
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

// RequirePermission 限制接口只能由持有指定权限码的当前身份访问。
// 权限码在认证中间件中实时计算，owner 与平台管理员旁路各自的全部权限码。
func RequirePermission(code string) gin.HandlerFunc {
	return RequireAnyPermission(code)
}

// RequireAnyPermission 限制接口只能由持有任一指定权限码的当前身份访问。
func RequireAnyPermission(codes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		current, ok := requestctx.Get(c)
		if !ok {
			httpx.Error(c, 401, 14010, "missing identity")
			return
		}
		for _, code := range codes {
			if current.HasPermission(code) {
				c.Next()
				return
			}
		}
		httpx.Error(c, 403, 14031, "permission denied: missing permission code")
	}
}
