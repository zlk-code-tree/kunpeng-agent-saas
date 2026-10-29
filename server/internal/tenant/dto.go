package tenant

type TenantCreateInput struct {
	Name string `json:"name" binding:"required,min=2,max=100"`
}

type RegisterInput struct {
	Username    string `json:"username" binding:"required,min=3,max=64"`
	DisplayName string `json:"displayName" binding:"required,min=2,max=64"`
	Password    string `json:"password" binding:"required,min=8,max=128"`
}

type InviteInput struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Role     string `json:"role" binding:"required,oneof=admin member finance"`
}
