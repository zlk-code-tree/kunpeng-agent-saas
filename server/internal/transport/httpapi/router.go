package httpapi

import (
	"gin-dify-saas/server/internal/agent"
	"gin-dify-saas/server/internal/billing"
	"gin-dify-saas/server/internal/execution"
	"gin-dify-saas/server/internal/identity"
	"gin-dify-saas/server/internal/installation"
	"gin-dify-saas/server/internal/integration"
	"gin-dify-saas/server/internal/knowledge"
	"gin-dify-saas/server/internal/operations"
	"gin-dify-saas/server/internal/platform/observability"
	"gin-dify-saas/server/internal/system"
	"gin-dify-saas/server/internal/tenant"
	"gin-dify-saas/server/internal/toolgateway"
	"gin-dify-saas/server/internal/transport/middleware"

	"github.com/gin-gonic/gin"
)

// Dependencies 是 HTTP 传输层需要的已装配依赖。业务服务的创建由 app 包负责，
// 路由层只负责协议、中间件与 URL 到处理器的映射。
type Dependencies struct {
	Environment  string
	CORSOrigins  []string
	Metrics      *observability.Metrics
	Authenticate gin.HandlerFunc

	Installation *installation.InstallationHandler
	Identity     *identity.IdentityHandler
	Tenant       *tenant.TenantHandler
	Billing      *billing.BillingHandler
	Agent        *agent.AgentHandler
	Knowledge    *knowledge.KnowledgeHandler
	Integration  *integration.IntegrationHandler
	ToolGateway  *toolgateway.ToolgatewayHandler
	Execution    *execution.ExecutionHandler
	Operations   *operations.OperationsHandler
	System       *system.SystemHandler
}

// NewRouter 注册公开接口、认证接口和平台管理接口。
func NewRouter(deps Dependencies) *gin.Engine {
	if deps.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(
		middleware.RequestID(),
		gin.Recovery(),
		deps.Metrics.Middleware(),
		middleware.RequestLogger(),
		middleware.CORS(deps.CORSOrigins),
	)

	router.GET("/healthz", deps.System.Health)
	router.GET("/readyz", deps.System.Ready)
	router.GET("/metrics", deps.Metrics.Handler)

	api := router.Group("/api/v1")
	api.GET("/install/status", deps.Installation.Status)
	api.POST("/install", deps.Installation.Setup)
	api.GET("/system/capabilities", deps.System.Capabilities)
	api.POST("/auth/login", deps.Identity.Login)
	api.POST("/auth/register", deps.Tenant.Register)
	// 工具调用故意不走普通 JWT：调用方持有与运行绑定的短期 HMAC 票据，
	// Invoke 会自行校验签名并基于数据库中的最新授权执行。
	api.POST("/internal/tools/invoke", deps.ToolGateway.Invoke)

	secured := api.Group("")
	secured.Use(deps.Authenticate)
	secured.POST("/auth/logout", deps.Identity.Logout)
	secured.GET("/auth/codes", deps.Identity.Codes)
	secured.GET("/user/info", deps.Identity.UserInfo)
	secured.GET("/agents", deps.Agent.List)
	secured.GET("/billing/account", deps.Billing.CurrentAccount)
	secured.GET("/billing/plans", deps.Billing.Plans)
	secured.GET("/billing/subscription", deps.Billing.CurrentSubscription)
	secured.GET("/billing/ledger", deps.Billing.Ledger)
	secured.GET("/tenants", deps.Tenant.List)
	secured.POST("/tenants", deps.Tenant.Create)
	secured.POST("/tenants/:id/switch", deps.Tenant.Switch)
	secured.GET("/tenants/:id/members", deps.Tenant.Members)
	secured.POST("/tenants/:id/invitations", deps.Tenant.Invite)
	secured.POST("/tenant-invitations/accept", deps.Tenant.AcceptInvitation)
	secured.DELETE("/tenants/:id/members/:userId", deps.Tenant.RemoveMember)
	secured.POST("/tenants/:id/leave", deps.Tenant.Leave)
	secured.POST("/tenants/:id/transfer-ownership", deps.Tenant.TransferOwnership)
	secured.GET("/knowledge-bases", deps.Knowledge.List)
	secured.POST("/knowledge-bases", deps.Knowledge.Create)
	secured.GET("/knowledge-bases/:id/documents", deps.Knowledge.ListDocuments)
	secured.POST("/knowledge-bases/:id/documents", deps.Knowledge.Upload)
	secured.POST("/runs", deps.Execution.Create)
	secured.GET("/runs/:id", deps.Execution.Get)
	secured.POST("/runs/:id/cancel", deps.Execution.Cancel)
	secured.GET("/runs/:id/events", deps.Execution.Events)
	secured.POST("/runs/:id/tool-tickets", deps.ToolGateway.Ticket)

	// 平台管理权限独立于租户 owner/admin，所有安装级操作必须再次验证平台管理员标记。
	admin := secured.Group("/admin")
	admin.Use(middleware.RequirePlatformAdmin())
	admin.GET("/dify-instances", deps.Integration.ListInstances)
	admin.POST("/dify-instances", deps.Integration.CreateInstance)
	admin.POST("/dify-instances/:id/health-check", deps.Integration.CheckInstance)
	admin.POST("/dify-instances/:id/rotate-credential", deps.Integration.RotateInstanceCredential)
	admin.POST("/dify-bindings", deps.Integration.BindApp)
	admin.GET("/model-providers", deps.Integration.ListProviders)
	admin.POST("/model-providers", deps.Integration.CreateProvider)
	admin.POST("/model-routes", deps.Integration.SetModelRoute)
	admin.POST("/model-providers/:id/prices", deps.Integration.SetModelPrice)
	admin.GET("/tools", deps.ToolGateway.List)
	admin.POST("/tools", deps.ToolGateway.Create)
	admin.POST("/tools/:id/grants", deps.ToolGateway.Grant)
	admin.POST("/billing/accounts/:id/adjustments", deps.Billing.Adjust)
	admin.GET("/costs/summary", deps.Billing.CostSummary)
	admin.POST("/reconciliation", deps.Billing.Reconcile)
	admin.POST("/tenants/:id/export", deps.Operations.ExportTenant)

	return router
}
