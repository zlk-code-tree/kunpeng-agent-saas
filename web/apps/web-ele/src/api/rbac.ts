import { requestClient } from '#/api/request';

export interface PermissionItem {
  code: string;
  description: string;
  group: string;
  name: string;
  scope: 'platform' | 'tenant';
  sortOrder: number;
}

export interface RoleItem {
  builtin: boolean;
  code: string;
  description: string;
  id: string;
  name: string;
  permissionCodes: string[];
  scope: 'platform' | 'tenant';
  tenantId: string;
}

export interface RolePayload {
  description?: string;
  name: string;
  permissionCodes: string[];
}

export interface PlatformUserPayload {
  displayName: string;
  isPlatformAdmin?: boolean;
  password: string;
  username: string;
}

/** 获取权限码目录（平台与租户作用域）。 */
export function listPermissionsApi() {
  return requestClient.get<PermissionItem[]>('/tenant/permissions');
}

/** 获取当前租户可用角色（内置 + 本租户自定义）。 */
export function listTenantRolesApi() {
  return requestClient.get<RoleItem[]>('/tenant/roles');
}

/** 创建租户自定义角色并绑定权限码。 */
export function createTenantRoleApi(data: RolePayload) {
  return requestClient.post<RoleItem>('/tenant/roles', data);
}

/** 更新自定义角色，权限码整体替换。 */
export function updateTenantRoleApi(roleId: string, data: RolePayload) {
  return requestClient.put<RoleItem>(`/tenant/roles/${roleId}`, data);
}

/** 删除租户自定义角色。 */
export function deleteTenantRoleApi(roleId: string) {
  return requestClient.delete<{ success: boolean }>(`/tenant/roles/${roleId}`);
}

/** 获取成员已分配的自定义角色 ID 列表。 */
export function getMemberRolesApi(userId: string) {
  return requestClient.get<{ roleIds: string[] }>(
    `/tenant/members/${userId}/roles`,
  );
}

/** 设置成员的自定义角色集合；传空数组表示清空。 */
export function setMemberRolesApi(userId: string, roleIds: string[]) {
  return requestClient.put<{ success: boolean }>(
    `/tenant/members/${userId}/roles`,
    { roleIds },
  );
}

/** 创建平台后台管理员账号，可同时授予安装级平台管理员。 */
export function createPlatformUserApi(data: PlatformUserPayload) {
  return requestClient.post('/admin/users', data);
}
