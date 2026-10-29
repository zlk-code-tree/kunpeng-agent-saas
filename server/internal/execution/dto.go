package execution

type ExecutionCreateInput struct {
	AgentVersionID string         `json:"agentVersionId" binding:"required,uuid"`
	ConversationID string         `json:"conversationId"`
	Query          string         `json:"query" binding:"required,min=1,max=20000"`
	Inputs         map[string]any `json:"inputs"`
}
