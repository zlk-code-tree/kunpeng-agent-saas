package httpapi

import (
	"gin-dify-saas/server/internal/administration"
	"gin-dify-saas/server/internal/agent"
	"gin-dify-saas/server/internal/billing"
	"gin-dify-saas/server/internal/execution"
	"gin-dify-saas/server/internal/identity"
	"gin-dify-saas/server/internal/installation"
	"gin-dify-saas/server/internal/integration"
	"gin-dify-saas/server/internal/knowledge"
	"gin-dify-saas/server/internal/operations"
	"gin-dify-saas/server/internal/platform/observability"
	"gin-dify-saas/server/internal/rbac"
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

	Installation   *installation.InstallationHandler
	Identity       *identity.IdentityHandler
	Tenant         *tenant.TenantHandler
	Billing        *billing.BillingHandler
	Agent          *agent.AgentHandler
	Knowledge      *knowledge.KnowledgeHandler
	Integration    *integration.IntegrationHandler
	ToolGateway    *toolgateway.ToolgatewayHandler
	Execution      *execution.ExecutionHandler
	Operations     *operations.OperationsHandler
	System         *system.SystemHandler
	Administration *administration.Handler
	RBAC           *rbac.Handler
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
	secured.GET("/menu/settings", deps.Administration.Menus)
	secured.GET("/agents", middleware.RequirePermission("agent:view"), deps.Agent.List)
	secured.GET("/billing/account", middleware.RequirePermission("billing:view"), deps.Billing.CurrentAccount)
	secured.GET("/billing/plans", middleware.RequirePermission("billing:view"), deps.Billing.Plans)
	secured.GET("/billing/subscription", middleware.RequirePermission("billing:view"), deps.Billing.CurrentSubscription)
	secured.GET("/billing/ledger", middleware.RequirePermission("billing:ledger-view"), deps.Billing.Ledger)
	secured.GET("/tenants", deps.Tenant.List)
	secured.POST("/tenants", middleware.RequirePermission("tenant:settings"), deps.Tenant.Create)
	secured.POST("/tenants/:id/switch", middleware.RequirePermission("tenant:switch"), deps.Tenant.Switch)
	secured.GET("/tenants/:id/members", middleware.RequirePermission("member:view"), deps.Tenant.Members)
	secured.POST("/tenants/:id/invitations", middleware.RequirePermission("member:invite"), deps.Tenant.Invite)
	secured.POST("/tenant-invitations/accept", deps.Tenant.AcceptInvitation)
	secured.DELETE("/tenants/:id/members/:userId", middleware.RequirePermission("member:remove"), deps.Tenant.RemoveMember)
	secured.PATCH("/tenants/:id/members/:userId/role", middleware.RequirePermission("member:role-assign"), deps.Tenant.UpdateMemberRole)
	secured.POST("/tenants/:id/leave", deps.Tenant.Leave)
	secured.POST("/tenants/:id/transfer-ownership", middleware.RequirePermission("member:role-assign"), deps.Tenant.TransferOwnership)
	secured.GET("/knowledge-bases", middleware.RequirePermission("knowledge:view"), deps.Knowledge.List)
	secured.POST("/knowledge-bases", middleware.RequirePermission("knowledge:create"), deps.Knowledge.Create)
	secured.GET("/knowledge-bases/:id/documents", middleware.RequirePermission("knowledge:view"), deps.Knowledge.ListDocuments)
	secured.POST("/knowledge-bases/:id/documents", middleware.RequirePermission("knowledge:upload"), deps.Knowledge.Upload)
	secured.POST("/runs", middleware.RequirePermission("agent:run"), deps.Execution.Create)
	secured.GET("/runs/:id", middleware.RequirePermission("run:view"), deps.Execution.Get)
	secured.POST("/runs/:id/cancel", middleware.RequirePermission("run:cancel"), deps.Execution.Cancel)
	secured.GET("/runs/:id/events", middleware.RequirePermission("run:view"), deps.Execution.Events)
	secured.POST("/runs/:id/tool-tickets", middleware.RequirePermission("run:tool-ticket"), deps.ToolGateway.Ticket)

	// 租户侧 RBAC：权限码目录与角色读取对全体成员开放，角色维护需要 role:manage，
	// 成员角色分配需要 member:role-assign；owner 旁路全部租户权限码。
	secured.GET("/tenant/permissions", deps.RBAC.Permissions)
	secured.GET("/tenant/roles", deps.RBAC.Roles)
	secured.POST("/tenant/roles", middleware.RequirePermission("role:manage"), deps.RBAC.CreateRole)
	secured.PUT("/tenant/roles/:roleId", middleware.RequirePermission("role:manage"), deps.RBAC.UpdateRole)
	secured.DELETE("/tenant/roles/:roleId", middleware.RequirePermission("role:manage"), deps.RBAC.DeleteRole)
	secured.PUT("/tenant/members/:userId/roles", middleware.RequirePermission("member:role-assign"), deps.RBAC.SetMemberRoles)
	secured.GET("/tenant/members/:userId/roles", middleware.RequirePermission("member:role-assign"), deps.RBAC.MemberRoles)

	// 平台管理权限独立于租户 owner/admin，所有安装级操作必须再次验证平台管理员标记；
	// RequirePermission 统一按平台权限码放行，与前端按钮显隐同源。
	admin := secured.Group("/admin")
	admin.Use(middleware.RequirePlatformAdmin())
	admin.GET("/menus", middleware.RequirePermission("menu:manage"), deps.Administration.Menus)
	admin.POST("/menus", middleware.RequirePermission("menu:manage"), deps.Administration.CreateMenu)
	admin.PUT("/menus/:routeName", middleware.RequirePermission("menu:manage"), deps.Administration.UpdateMenu)
	admin.DELETE("/menus/:routeName", middleware.RequirePermission("menu:manage"), deps.Administration.DeleteMenu)
	admin.GET("/users", middleware.RequirePermission("user:manage"), deps.Administration.Users)
	admin.POST("/users", middleware.RequirePermission("user:manage"), deps.Administration.CreateUser)
	admin.PATCH("/users/:userId", middleware.RequirePermission("user:manage"), deps.Administration.UpdateUser)
	admin.PATCH("/users/:userId/platform-admin", middleware.RequirePermission("user:platform-admin"), deps.Administration.SetPlatformAdmin)
	admin.GET("/dify-instances", middleware.RequirePermission("instance:manage"), deps.Integration.ListInstances)
	admin.POST("/dify-instances", middleware.RequirePermission("instance:manage"), deps.Integration.CreateInstance)
	admin.POST("/dify-instances/:id/health-check", middleware.RequirePermission("instance:manage"), deps.Integration.CheckInstance)
	admin.POST("/dify-instances/:id/rotate-credential", middleware.RequirePermission("instance:manage"), deps.Integration.RotateInstanceCredential)
	admin.POST("/dify-bindings", middleware.RequirePermission("instance:manage"), deps.Integration.BindApp)
	admin.GET("/model-providers", middleware.RequirePermission("provider:manage"), deps.Integration.ListProviders)
	admin.POST("/model-providers", middleware.RequirePermission("provider:manage"), deps.Integration.CreateProvider)
	admin.POST("/model-routes", middleware.RequirePermission("route:manage"), deps.Integration.SetModelRoute)
	admin.POST("/model-providers/:id/prices", middleware.RequirePermission("price:manage"), deps.Integration.SetModelPrice)
	admin.GET("/tools", middleware.RequirePermission("tool:manage"), deps.ToolGateway.List)
	admin.POST("/tools", middleware.RequirePermission("tool:manage"), deps.ToolGateway.Create)
	admin.POST("/tools/:id/grants", middleware.RequirePermission("grant:manage"), deps.ToolGateway.Grant)
	admin.POST("/billing/accounts/:id/adjustments", middleware.RequirePermission("billing:adjust"), deps.Billing.Adjust)
	admin.GET("/costs/summary", middleware.RequirePermission("cost:view"), deps.Billing.CostSummary)
	admin.POST("/reconciliation", middleware.RequirePermission("reconcile"), deps.Billing.Reconcile)
	admin.POST("/tenants/:id/export", middleware.RequirePermission("tenant:export"), deps.Operations.ExportTenant)

	return router
}
