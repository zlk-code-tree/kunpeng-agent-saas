import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'MenuManagement',
    path: '/administration/menus',
    component: () => import('#/views/administration/menus.vue'),
    meta: {
      authority: ['platform_admin'],
      icon: 'lucide:panel-left',
      order: 40,
      title: '菜单管理',
    },
  },
  {
    name: 'PermissionManagement',
    path: '/administration/permissions',
    component: () => import('#/views/administration/permissions.vue'),
    meta: {
      authority: ['platform_admin'],
      icon: 'lucide:shield-check',
      order: 45,
      title: '权限管理',
    },
  },
];

export default routes;
