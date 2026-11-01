import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'TenantManagement',
    path: '/tenants',
    component: () => import('#/views/tenants/index.vue'),
    meta: {
      authority: ['member:view'],
      icon: 'lucide:building-2',
      order: 5,
      title: '空间与成员',
    },
  },
  {
    name: 'RoleManagement',
    path: '/tenants/roles',
    component: () => import('#/views/tenants/roles.vue'),
    meta: {
      authority: ['role:manage'],
      icon: 'lucide:key-round',
      order: 6,
      title: '角色管理',
    },
  },
];

export default routes;
