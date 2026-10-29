package installation

type Status struct {
	Installed       bool   `json:"installed"`
	Mode            string `json:"mode"`
	InstallationID  string `json:"installationId,omitempty"`
	DefaultTenantID string `json:"defaultTenantId,omitempty"`
}
