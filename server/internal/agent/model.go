package agent

type AgentItem struct {
	ID              string         `json:"id"`
	AgentVersionID  string         `json:"agentVersionId"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Category        string         `json:"category"`
	Version         int            `json:"version"`
	ParameterSchema map[string]any `json:"parameterSchema"`
	Enabled         bool           `json:"enabled"`
	Configured      bool           `json:"configured"`
}
