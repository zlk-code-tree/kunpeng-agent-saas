package installation

type SetupInput struct {
	PlatformName  string `json:"platformName" binding:"required,min=2,max=100"`
	TenantName    string `json:"tenantName" binding:"required,min=2,max=100"`
	AdminUsername string `json:"adminUsername" binding:"required,min=3,max=64"`
	AdminName     string `json:"adminName" binding:"required,min=2,max=64"`
	AdminPassword string `json:"adminPassword" binding:"required,min=8,max=128"`
}
