package requestctx

import "github.com/gin-gonic/gin"

const key = "request_identity"

type Identity struct {
	InstallationID string
	TenantID       string
	UserID         string
	Username       string
	Roles          []string
	PlatformAdmin  bool
	// PermissionCodes 是当前请求实时计算的权限码集合，驱动接口放行与前端按钮显隐。
	PermissionCodes []string
}

// HasPermission 判断当前身份是否持有指定权限码。
func (i Identity) HasPermission(code string) bool {
	for _, item := range i.PermissionCodes {
		if item == code {
			return true
		}
	}
	return false
}

// Set 将认证后的当前用户、租户和角色信息写入本次 Gin 请求上下文。
func Set(c *gin.Context, identity Identity) {
	c.Set(key, identity)
}

// Get 从 Gin 请求上下文读取当前身份；返回 false 表示认证中间件尚未写入身份。
func Get(c *gin.Context) (Identity, bool) {
	value, ok := c.Get(key)
	if !ok {
		return Identity{}, false
	}
	identity, ok := value.(Identity)
	return identity, ok
}
