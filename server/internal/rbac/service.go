package rbac

import (
	"context"
	"errors"
	"strings"

	"gin-dify-saas/server/internal/platform/ids"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound       = errors.New("role not found")
	ErrBuiltinRole    = errors.New("built-in role cannot be modified")
	ErrScopeViolation = errors.New("permission codes must match the role scope")
	ErrConflict       = errors.New("role already exists")
	ErrMemberNotFound = errors.New("tenant member not found")
	ErrInvalidRoleRef = errors.New("role does not belong to this tenant")
)

type Service struct{ db *pgxpool.Pool }

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Permissions 返回完整权限码目录，前端按 scope 过滤后渲染角色勾选面板。
func (s *Service) Permissions(ctx context.Context) ([]Permission, error) {
	rows, err := s.db.Query(ctx, `
		SELECT code, name, scope, group_name, description, sort_order
		FROM permissions ORDER BY scope, sort_order, code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Permission, 0)
	for rows.Next() {
		var item Permission
		if err := rows.Scan(&item.Code, &item.Name, &item.Scope, &item.Group, &item.Description, &item.SortOrder); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Roles 返回当前租户可见的角色：四个内置角色加本租户的自定义角色，均携带权限码。
func (s *Service) Roles(ctx context.Context, tenantID string) ([]Role, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.id::text, r.code, r.name, r.scope, r.builtin, COALESCE(r.tenant_id::text, ''), r.description,
		       COALESCE(ARRAY(
		           SELECT rp.permission_code FROM role_permissions rp
		           WHERE rp.role_id = r.id ORDER BY rp.permission_code
		       ), '{}')
		FROM roles r
		WHERE r.scope = 'tenant' AND (r.tenant_id IS NULL OR r.tenant_id = $1)
		ORDER BY r.builtin DESC, r.created_at
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Role, 0)
	for rows.Next() {
		var item Role
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Scope, &item.Builtin, &item.TenantID, &item.Description, &item.PermissionCodes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CreateRole 新建租户自定义角色并绑定权限码。权限码必须全部为 tenant 作用域（scope 隔离铁律）。
func (s *Service) CreateRole(ctx context.Context, tenantID, actorID string, input CreateRoleInput) (Role, error) {
	codes, err := s.validateTenantCodes(ctx, input.PermissionCodes)
	if err != nil {
		return Role{}, err
	}
	id, _ := ids.New()
	code := "custom-" + strings.ReplaceAll(id, "-", "")[:12]
	role := Role{
		ID: id, Code: code, Name: strings.TrimSpace(input.Name), Scope: "tenant",
		TenantID: tenantID, Description: strings.TrimSpace(input.Description),
		PermissionCodes: codes,
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Role{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO roles (id, code, name, scope, builtin, tenant_id, description)
		VALUES ($1,$2,$3,'tenant',FALSE,$4,$5)
	`, role.ID, role.Code, role.Name, tenantID, role.Description); err != nil {
		return Role{}, err
	}
	if err := bindPermissions(ctx, tx, role.ID, codes); err != nil {
		return Role{}, err
	}
	if err := insertRoleAudit(ctx, tx, tenantID, actorID, "rbac.role.create", role.ID, role.Name, codes); err != nil {
		return Role{}, err
	}
	return role, tx.Commit(ctx)
}

// UpdateRole 更新自定义角色的名称、描述和权限码；内置角色受保护不可修改。
func (s *Service) UpdateRole(ctx context.Context, tenantID, roleID, actorID string, input UpdateRoleInput) (Role, error) {
	role, err := s.tenantRole(ctx, tenantID, roleID)
	if err != nil {
		return Role{}, err
	}
	if role.Builtin {
		return Role{}, ErrBuiltinRole
	}
	codes, err := s.validateTenantCodes(ctx, input.PermissionCodes)
	if err != nil {
		return Role{}, err
	}
	role.Name = strings.TrimSpace(input.Name)
	role.Description = strings.TrimSpace(input.Description)
	role.PermissionCodes = codes
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Role{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE roles SET name=$2, description=$3, updated_at=NOW() WHERE id=$1
	`, role.ID, role.Name, role.Description); err != nil {
		return Role{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id=$1`, role.ID); err != nil {
		return Role{}, err
	}
	if err := bindPermissions(ctx, tx, role.ID, codes); err != nil {
		return Role{}, err
	}
	if err := insertRoleAudit(ctx, tx, tenantID, actorID, "rbac.role.update", role.ID, role.Name, codes); err != nil {
		return Role{}, err
	}
	return role, tx.Commit(ctx)
}

// DeleteRole 删除自定义角色；级联清理角色权限绑定与成员角色绑定。
func (s *Service) DeleteRole(ctx context.Context, tenantID, roleID, actorID string) error {
	role, err := s.tenantRole(ctx, tenantID, roleID)
	if err != nil {
		return err
	}
	if role.Builtin {
		return ErrBuiltinRole
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM roles WHERE id=$1`, role.ID); err != nil {
		return err
	}
	if err := insertRoleAudit(ctx, tx, tenantID, actorID, "rbac.role.delete", role.ID, role.Name, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetMemberRoles 为成员设置自定义角色集合。内置角色行随 tenant_members.role 同步重建，
// 保证 member_roles 始终完整表达成员的角色分配，同时不影响既有角色切换逻辑。
func (s *Service) SetMemberRoles(ctx context.Context, tenantID, actorID, targetUserID string, roleIDs []string) error {
	var builtinRole string
	err := s.db.QueryRow(ctx, `
		SELECT role FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active'
	`, tenantID, targetUserID).Scan(&builtinRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMemberNotFound
	}
	if err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		role, err := s.tenantRole(ctx, tenantID, roleID)
		if err != nil {
			return err
		}
		if role.Builtin && role.Code != builtinRole {
			// 内置角色由成员主角色列决定，不能通过附加绑定旁路升级。
			return ErrInvalidRoleRef
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM member_roles WHERE tenant_id=$1 AND user_id=$2`, tenantID, targetUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO member_roles (tenant_id, user_id, role_id)
		SELECT $1, $2, id FROM roles WHERE code=$3 AND scope='tenant' AND tenant_id IS NULL
		ON CONFLICT DO NOTHING
	`, tenantID, targetUserID, builtinRole); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO member_roles (tenant_id, user_id, role_id) VALUES ($1,$2,$3)
			ON CONFLICT DO NOTHING
		`, tenantID, targetUserID, roleID); err != nil {
			return err
		}
	}
	id, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (id, tenant_id, actor_user_id, action, resource_type, resource_id, metadata)
		VALUES ($1,$2,$3,'rbac.member.roles.update','tenant_member',$4,jsonb_build_object('roleIds',$5))
	`, id, tenantID, actorID, targetUserID, roleIDs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MemberCustomRoleIDs 返回成员已分配的自定义角色 ID 列表；
// 内置角色由 tenant_members.role 表达，不在返回结果内。
func (s *Service) MemberCustomRoleIDs(ctx context.Context, tenantID, targetUserID string) ([]string, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM tenant_members WHERE tenant_id=$1 AND user_id=$2 AND status='active')
	`, tenantID, targetUserID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrMemberNotFound
	}
	rows, err := s.db.Query(ctx, `
		SELECT mr.role_id::text FROM member_roles mr
		JOIN roles r ON r.id = mr.role_id
		WHERE mr.tenant_id=$1 AND mr.user_id=$2 AND r.tenant_id IS NOT NULL
		ORDER BY r.name
	`, tenantID, targetUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roleIDs := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		roleIDs = append(roleIDs, id)
	}
	return roleIDs, rows.Err()
}

// tenantRole 返回属于当前租户（或内置共享）的角色定义。
func (s *Service) tenantRole(ctx context.Context, tenantID, roleID string) (Role, error) {
	var role Role
	err := s.db.QueryRow(ctx, `
		SELECT id::text, code, name, scope, builtin, COALESCE(tenant_id::text, ''), description
		FROM roles
		WHERE id=$1 AND scope='tenant' AND (tenant_id IS NULL OR tenant_id=$2)
	`, roleID, tenantID).Scan(&role.ID, &role.Code, &role.Name, &role.Scope, &role.Builtin, &role.TenantID, &role.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return Role{}, ErrNotFound
	}
	if err != nil {
		return Role{}, err
	}
	return role, nil
}

// validateTenantCodes 校验权限码存在且均为 tenant 作用域，并去重稳定排序。
func (s *Service) validateTenantCodes(ctx context.Context, codes []string) ([]string, error) {
	seen := make(map[string]struct{}, len(codes))
	unique := make([]string, 0, len(codes))
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		unique = append(unique, code)
	}
	if len(unique) == 0 {
		return nil, ErrScopeViolation
	}
	rows, err := s.db.Query(ctx, `
		SELECT code FROM permissions WHERE scope='tenant' AND code = ANY($1)
	`, unique)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	valid := make(map[string]struct{}, len(unique))
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		valid[code] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, code := range unique {
		if _, ok := valid[code]; !ok {
			return nil, ErrScopeViolation
		}
	}
	return unique, nil
}

// bindPermissions 在调用方事务内写入角色-权限码绑定。
func bindPermissions(ctx context.Context, tx pgx.Tx, roleID string, codes []string) error {
	for _, code := range codes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO role_permissions (role_id, permission_code) VALUES ($1,$2)
			ON CONFLICT DO NOTHING
		`, roleID, code); err != nil {
			return err
		}
	}
	return nil
}

// insertRoleAudit 在调用方事务内记录角色变更审计。
func insertRoleAudit(ctx context.Context, tx pgx.Tx, tenantID, actorID, action, roleID, roleName string, codes []string) error {
	id, _ := ids.New()
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (id, tenant_id, actor_user_id, action, resource_type, resource_id, metadata)
		VALUES ($1,$2,$3,$4,'role',$5,jsonb_build_object('name',$6,'permissionCodes',$7))
	`, id, tenantID, actorID, action, roleID, roleName, codes)
	return err
}
