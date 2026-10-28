import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'EnterpriseOperations',
    path: '/enterprise',
    component: () => import('#/views/enterprise/index.vue'),
    meta: {
      authority: ['platform_admin'],
      icon: 'lucide:network',
      order: 35,
      title: '企业集成与成本',
    },
  },
];

export default routes;
