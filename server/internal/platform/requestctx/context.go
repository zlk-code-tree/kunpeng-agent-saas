package requestctx

import "github.com/gin-gonic/gin"

const key = "request_identity"

type Identity struct {
	InstallationID string
	TenantID       string
	UserID         string
	Username       string
	Roles          []string
}

func Set(c *gin.Context, identity Identity) {
	c.Set(key, identity)
}

func Get(c *gin.Context) (Identity, bool) {
	value, ok := c.Get(key)
	if !ok {
		return Identity{}, false
	}
	identity, ok := value.(Identity)
	return identity, ok
}
