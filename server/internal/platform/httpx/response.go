package httpx

import "github.com/gin-gonic/gin"

type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(200, Response{Code: 0, Message: "ok", Data: data})
}

func Created(c *gin.Context, data any) {
	c.JSON(201, Response{Code: 0, Message: "ok", Data: data})
}

func Error(c *gin.Context, status, code int, message string) {
	c.AbortWithStatusJSON(status, Response{Code: code, Message: message})
}
