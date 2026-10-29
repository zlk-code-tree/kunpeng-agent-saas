package httpx

import "github.com/gin-gonic/gin"

type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// OK 返回统一格式的 HTTP 200 成功响应。
func OK(c *gin.Context, data any) {
	c.JSON(200, Response{Code: 0, Message: "ok", Data: data})
}

// Created 返回统一格式的 HTTP 201 资源创建成功响应。
func Created(c *gin.Context, data any) {
	c.JSON(201, Response{Code: 0, Message: "ok", Data: data})
}

// Error 中止后续处理并返回统一格式的业务错误响应。
func Error(c *gin.Context, status, code int, message string) {
	c.AbortWithStatusJSON(status, Response{Code: code, Message: message})
}
