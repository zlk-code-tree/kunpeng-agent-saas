import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'SystemManagement',
    path: '/system-management',
    meta: {
      // 系统管理作为统一导航分组：平台管理员与租户角色管理员均可进入，
      // 具体页面仍由各子路由的权限码独立过滤。
      authority: ['platform_admin', 'role:manage'],
      icon: 'lucide:settings-2',
      order: 30,
      title: '系统管理',
    },
    children: [
      {
        name: 'SystemOverview',
        path: '/system',
        component: () => import('#/views/system/index.vue'),
        meta: {
          authority: ['platform_admin'],
          icon: 'lucide:sliders-horizontal',
          title: '平台设置',
        },
      },
      {
        name: 'UserManagement',
        path: '/administration/users',
        component: () => import('#/views/administration/users.vue'),
        meta: {
          authority: ['user:manage'],
          icon: 'lucide:users-round',
          title: '用户管理',
        },
      },
      {
        name: 'RoleManagement',
        path: '/tenants/roles',
        component: () => import('#/views/tenants/roles.vue'),
        meta: {
          authority: ['role:manage'],
          icon: 'lucide:key-round',
          title: '角色管理',
        },
      },
      {
        name: 'MenuManagement',
        path: '/administration/menus',
        component: () => import('#/views/administration/menus.vue'),
        meta: {
          authority: ['menu:manage'],
          icon: 'lucide:panel-left',
          title: '菜单管理',
        },
      },
      {
        name: 'PermissionManagement',
        path: '/administration/permissions',
        component: () => import('#/views/administration/permissions.vue'),
        meta: {
          authority: ['user:manage'],
          icon: 'lucide:shield-check',
          title: '权限管理',
        },
      },
    ],
  },
];

export default routes;
