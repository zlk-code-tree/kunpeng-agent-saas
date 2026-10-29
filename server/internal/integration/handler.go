package integration

import (
	"gin-dify-saas/server/internal/transport/httpx"
	"gin-dify-saas/server/internal/transport/requestctx"

	"github.com/gin-gonic/gin"
)

type IntegrationHandler struct {
	service *IntegrationService
}

// NewIntegrationHandler 创建企业集成管理接口处理器。
func NewIntegrationHandler(service *IntegrationService) *IntegrationHandler {
	return &IntegrationHandler{service: service}
}

// ListInstances 返回当前租户可见的 Dify 实例。
func (h *IntegrationHandler) ListInstances(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	items, err := h.service.ListInstances(c.Request.Context(), identity.TenantID)
	if err != nil {
		httpx.Error(c, 500, 15300, "failed to list Dify instances")
		return
	}
	httpx.OK(c, items)
}

// CreateInstance 处理 Dify 实例创建请求。
func (h *IntegrationHandler) CreateInstance(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	var input CreateInstanceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14300, "invalid Dify instance input")
		return
	}
	item, err := h.service.CreateInstance(c.Request.Context(), identity.TenantID, input)
	if err != nil {
		httpx.Error(c, 500, 15301, "failed to create Dify instance")
		return
	}
	httpx.Created(c, item)
}

// BindApp 处理智能体版本与 Dify 应用绑定请求。
func (h *IntegrationHandler) BindApp(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	var input BindAppInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14302, "invalid Dify app binding")
		return
	}
	if err := h.service.BindApp(c.Request.Context(), identity.TenantID, input); err != nil {
		httpx.Error(c, 500, 15302, "failed to bind Dify app")
		return
	}
	httpx.Created(c, map[string]bool{"success": true})
}

// CheckInstance 立即检查指定 Dify 实例的健康状态。
func (h *IntegrationHandler) CheckInstance(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	item, err := h.service.CheckInstance(c.Request.Context(), identity.TenantID, c.Param("id"))
	if err != nil {
		httpx.Error(c, 502, 15303, "Dify health check failed")
		return
	}
	httpx.OK(c, item)
}

// RotateInstanceCredential 处理 Dify 凭据轮换请求。
func (h *IntegrationHandler) RotateInstanceCredential(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	var input RotateCredentialInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14303, "invalid Dify credential")
		return
	}
	if err := h.service.RotateInstanceCredential(c.Request.Context(), identity.TenantID, c.Param("id"), input.APIKey); err != nil {
		httpx.Error(c, 404, 14304, "Dify instance not found")
		return
	}
	httpx.OK(c, map[string]bool{"success": true})
}

// CreateProvider 处理模型供应商创建请求。
func (h *IntegrationHandler) CreateProvider(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	var input CreateProviderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14310, "invalid model provider input")
		return
	}
	item, err := h.service.CreateProvider(c.Request.Context(), identity.TenantID, input)
	if err != nil {
		httpx.Error(c, 500, 15310, "failed to create model provider")
		return
	}
	httpx.Created(c, item)
}

// ListProviders 返回当前租户可用的模型供应商。
func (h *IntegrationHandler) ListProviders(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	items, err := h.service.ListProviders(c.Request.Context(), identity.TenantID)
	if err != nil {
		httpx.Error(c, 500, 15311, "failed to list model providers")
		return
	}
	httpx.OK(c, items)
}

// SetModelRoute 为智能体版本配置模型路由。
func (h *IntegrationHandler) SetModelRoute(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	var input SetModelRouteInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14312, "invalid model route")
		return
	}
	if err := h.service.SetModelRoute(c.Request.Context(), identity.TenantID, input); err != nil {
		httpx.Error(c, 409, 14313, err.Error())
		return
	}
	httpx.Created(c, map[string]bool{"success": true})
}

// SetModelPrice 发布模型成本价格版本。
func (h *IntegrationHandler) SetModelPrice(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	var input SetModelPriceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14314, "invalid model price")
		return
	}
	if err := h.service.SetModelPrice(c.Request.Context(), identity.TenantID, c.Param("id"), input); err != nil {
		httpx.Error(c, 409, 14315, err.Error())
		return
	}
	httpx.Created(c, map[string]bool{"success": true})
}
