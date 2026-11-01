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
	// RoleID 可选，指定接受邀请后附带的自定义角色（内置角色由 Role 表达）。
	RoleID string `json:"roleId" binding:"omitempty,uuid"`
}

// UpdateMemberRoleInput 只允许修改普通成员角色；所有者必须走单独的所有权转移流程。
type UpdateMemberRoleInput struct {
	Role string `json:"role" binding:"required,oneof=admin member finance"`
}
