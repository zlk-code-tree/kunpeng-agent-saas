package toolgateway

type CreateToolInput struct {
	Code             string         `json:"code" binding:"required,min=2,max=64"`
	Name             string         `json:"name" binding:"required,min=2,max=100"`
	Description      string         `json:"description"`
	Protocol         string         `json:"protocol" binding:"required,oneof=http_json mcp_http"`
	Endpoint         string         `json:"endpoint" binding:"required,url"`
	Method           string         `json:"method" binding:"omitempty,oneof=GET POST PUT PATCH"`
	Credential       string         `json:"credential"`
	InputSchema      map[string]any `json:"inputSchema"`
	OutputSchema     map[string]any `json:"outputSchema"`
	TimeoutMS        int            `json:"timeoutMs" binding:"omitempty,min=100,max=120000"`
	MaxResponseBytes int            `json:"maxResponseBytes" binding:"omitempty,min=1024,max=10485760"`
}

type GrantInput struct {
	TenantID               string   `json:"tenantId" binding:"required,uuid"`
	AllowedAgentVersionIDs []string `json:"allowedAgentVersionIds"`
	MaxCallsPerRun         int      `json:"maxCallsPerRun" binding:"omitempty,min=1,max=1000"`
}

type TicketInput struct {
	ToolCode string `json:"toolCode" binding:"required,min=2,max=64"`
}

type InvokeInput struct {
	Arguments map[string]any `json:"arguments"`
}
