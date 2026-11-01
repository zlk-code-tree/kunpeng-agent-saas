import { requestClient } from '#/api/request';

export interface AdminMenuItem {
  enabled: boolean;
  icon: string;
  linkUrl: string;
  menuType: 'builtin' | 'link';
  openMode: 'iframe' | 'window';
  protected: boolean;
  routeName: string;
  sortOrder: number;
  title: string;
}

export interface PlatformUserItem {
  createdAt: string;
  displayName: string;
  id: string;
  isPlatformAdmin: boolean;
  status: string;
  username: string;
}

export interface UpdatePlatformUserPayload {
  displayName: string;
  password?: string;
  status: 'active' | 'disabled';
}

/** 获取当前已登录用户的菜单显示配置。 */
export function listMenuSettingsApi() {
  return requestClient.get<AdminMenuItem[]>('/menu/settings');
}

/** 新增自定义菜单入口（外链或内嵌页面）。 */
export function createAdminMenuApi(data: {
  enabled: boolean;
  icon?: string;
  linkUrl: string;
  openMode: 'iframe' | 'window';
  sortOrder: number;
  title: string;
}) {
  return requestClient.post<AdminMenuItem>('/admin/menus', data);
}

/** 保存菜单的标题、排序、显示状态与链接设置。 */
export function updateAdminMenuApi(
  routeName: string,
  data: Partial<
    Pick<
      AdminMenuItem,
      'enabled' | 'icon' | 'linkUrl' | 'openMode' | 'sortOrder' | 'title'
    >
  >,
) {
  return requestClient.put<AdminMenuItem>(`/admin/menus/${routeName}`, data);
}

/** 删除自定义菜单；内置路由菜单不可删除。 */
export function deleteAdminMenuApi(routeName: string) {
  return requestClient.delete<{ success: boolean }>(
    `/admin/menus/${routeName}`,
  );
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

/** 修改后台账号资料、启用状态，并可选重置密码。 */
export function updatePlatformUserApi(
  userId: string,
  data: UpdatePlatformUserPayload,
) {
  return requestClient.request<PlatformUserItem>(`/admin/users/${userId}`, {
    data,
    method: 'PATCH',
  });
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
