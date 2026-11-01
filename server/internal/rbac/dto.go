package rbac

// Permission 是权限码目录项；scope 决定它只能绑定到对应作用域的角色上。
type Permission struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Scope       string `json:"scope"`
	Group       string `json:"group"`
	Description string `json:"description"`
	SortOrder   int    `json:"sortOrder"`
}

// Role 是角色定义；builtin 角色不可修改或删除，自定义角色挂在具体租户下。
type Role struct {
	ID              string   `json:"id"`
	Code            string   `json:"code"`
	Name            string   `json:"name"`
	Scope           string   `json:"scope"`
	Builtin         bool     `json:"builtin"`
	TenantID        string   `json:"tenantId"`
	Description     string   `json:"description"`
	PermissionCodes []string `json:"permissionCodes"`
}

// CreateRoleInput 新建租户自定义角色；权限码必须全部为 tenant 作用域。
type CreateRoleInput struct {
	Name            string   `json:"name" binding:"required,min=2,max=80"`
	Description     string   `json:"description" binding:"max=255"`
	PermissionCodes []string `json:"permissionCodes" binding:"required,min=1,max=64,dive,max=64"`
}

// UpdateRoleInput 更新自定义角色；权限码整体替换。
type UpdateRoleInput struct {
	Name            string   `json:"name" binding:"required,min=2,max=80"`
	Description     string   `json:"description" binding:"max=255"`
	PermissionCodes []string `json:"permissionCodes" binding:"required,min=1,max=64,dive,max=64"`
}

// SetMemberRolesInput 为成员设置角色集合（内置角色随 tenant_members.role 自动维护）。
// 传空数组表示移除该成员的全部自定义角色。
type SetMemberRolesInput struct {
	RoleIDs []string `json:"roleIds" binding:"omitempty,max=16,dive,uuid"`
}
