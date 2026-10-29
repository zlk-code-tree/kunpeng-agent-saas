package integration

type CreateInstanceInput struct {
	Name           string `json:"name" binding:"required,min=2,max=100"`
	BaseURL        string `json:"baseUrl" binding:"required,url"`
	APIKey         string `json:"apiKey" binding:"required,min=8"`
	DeploymentType string `json:"deploymentType" binding:"omitempty,oneof=shared dedicated"`
}

type BindAppInput struct {
	AgentVersionID string `json:"agentVersionId" binding:"required,uuid"`
	DifyInstanceID string `json:"difyInstanceId" binding:"required,uuid"`
	UpstreamAppID  string `json:"upstreamAppId"`
	APIKey         string `json:"apiKey" binding:"required,min=8"`
}

type RotateCredentialInput struct {
	APIKey string `json:"apiKey" binding:"required,min=8"`
}

type CreateProviderInput struct {
	Code         string `json:"code" binding:"required,min=2,max=64"`
	Name         string `json:"name" binding:"required,min=2,max=100"`
	ProviderType string `json:"providerType" binding:"required,oneof=openai anthropic deepseek ollama vllm compatible"`
	BaseURL      string `json:"baseUrl" binding:"required,url"`
	APIKey       string `json:"apiKey"`
	Shared       bool   `json:"shared"`
}

type SetModelRouteInput struct {
	AgentVersionID string         `json:"agentVersionId" binding:"required,uuid"`
	ProviderID     string         `json:"providerId" binding:"required,uuid"`
	Model          string         `json:"model" binding:"required,min=1,max=128"`
	Priority       int            `json:"priority" binding:"omitempty,min=1,max=1000"`
	Config         map[string]any `json:"config"`
}

type SetModelPriceInput struct {
	Model         string `json:"model" binding:"required,min=1,max=128"`
	InputPer1000  string `json:"inputPer1000" binding:"required"`
	OutputPer1000 string `json:"outputPer1000" binding:"required"`
	Currency      string `json:"currency" binding:"omitempty,max=16"`
}
