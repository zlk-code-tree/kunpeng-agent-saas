package installation

import (
	"errors"

	"gin-dify-saas/server/internal/transport/httpx"

	"github.com/gin-gonic/gin"
)

type InstallationHandler struct {
	service *InstallationService
}

// NewInstallationHandler 创建平台安装接口处理器。
func NewInstallationHandler(service *InstallationService) *InstallationHandler {
	return &InstallationHandler{service: service}
}

// Status 返回平台当前安装状态和运行模式。
func (h *InstallationHandler) Status(c *gin.Context) {
	status, err := h.service.Status(c.Request.Context())
	if err != nil {
		httpx.Error(c, 500, 15001, "failed to load installation status")
		return
	}
	httpx.OK(c, status)
}

// Setup 处理一次性平台初始化请求。
func (h *InstallationHandler) Setup(c *gin.Context) {
	var input SetupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14001, err.Error())
		return
	}
	status, err := h.service.Setup(c.Request.Context(), input)
	if errors.Is(err, ErrAlreadyInstalled) {
		httpx.Error(c, 409, 14009, err.Error())
		return
	}
	if err != nil {
		httpx.Error(c, 500, 15002, "failed to initialize platform")
		return
	}
	httpx.Created(c, status)
}
