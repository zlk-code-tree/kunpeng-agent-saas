import { requestClient } from '#/api/request';

export interface AdminMenuItem {
  enabled: boolean;
  protected: boolean;
  routeName: string;
  sortOrder: number;
  title: string;
}

export interface PlatformUserItem {
  displayName: string;
  id: string;
  isPlatformAdmin: boolean;
  status: string;
  username: string;
}

/** 获取当前已登录用户的菜单显示配置。 */
export function listMenuSettingsApi() {
  return requestClient.get<AdminMenuItem[]>('/menu/settings');
}

/** 保存已注册路由的菜单设置。 */
export function updateAdminMenuApi(
  routeName: string,
  data: Pick<AdminMenuItem, 'enabled' | 'sortOrder' | 'title'>,
) {
  return requestClient.put<AdminMenuItem>(`/admin/menus/${routeName}`, data);
}

/** 获取用户的安装级平台管理员状态。 */
export function listPlatformUsersApi() {
  return requestClient.get<PlatformUserItem[]>('/admin/users');
}

/** 授予或撤销安装级平台管理员角色。 */
export function setPlatformAdminApi(userId: string, enabled: boolean) {
  return requestClient.request<{ enabled: boolean }>(
    `/admin/users/${userId}/platform-admin`,
    { data: { enabled }, method: 'PATCH' },
  );
}

/** 租户所有者调整当前租户成员角色。 */
export function updateTenantMemberRoleApi(
  tenantId: string,
  userId: string,
  role: 'admin' | 'finance' | 'member',
) {
  return requestClient.request<{ role: string }>(
    `/tenants/${tenantId}/members/${userId}/role`,
    { data: { role }, method: 'PATCH' },
  );
}
