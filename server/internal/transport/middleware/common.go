package middleware

import (
	"net/http"
	"strings"
	"time"

	"gin-dify-saas/server/internal/platform/ids"

	"github.com/gin-gonic/gin"
)

// requestLogger 输出包含请求 ID、方法、路径、状态和耗时的访问日志。
func RequestLogger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return strings.Join([]string{
			param.TimeStamp.Format(time.RFC3339),
			param.Request.Header.Get("X-Request-ID"),
			param.Method,
			param.Path,
			param.StatusCodeColor() + http.StatusText(param.StatusCode) + param.ResetColor(),
			param.Latency.String(),
			"\n",
		}, " ")
	})
}

// requestID 接受合法的上游请求 ID，否则生成新 UUID，并在响应头中回传。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if !validRequestID(requestID) {
			requestID, _ = ids.New()
		}
		c.Request.Header.Set("X-Request-ID", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// validRequestID 限制请求 ID 的长度和字符集，防止日志注入。
func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

// cors 只对配置白名单中的 Origin 返回跨域凭据许可，并统一处理预检请求。
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if _, ok := allowed[origin]; ok {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept-Language, X-Request-ID, Idempotency-Key, Last-Event-ID")
			c.Header("Access-Control-Expose-Headers", "X-Request-ID")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
