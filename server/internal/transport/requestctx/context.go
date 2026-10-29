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
