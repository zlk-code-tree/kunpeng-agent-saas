import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'TenantManagement',
    path: '/tenants',
    component: () => import('#/views/tenants/index.vue'),
    meta: {
      icon: 'lucide:building-2',
      order: 5,
      title: '空间与成员',
    },
  },
];

export default routes;
