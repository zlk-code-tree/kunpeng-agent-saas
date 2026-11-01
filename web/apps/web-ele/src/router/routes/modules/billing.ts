import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'BillingOverview',
    path: '/billing',
    component: () => import('#/views/billing/index.vue'),
    meta: {
      authority: ['billing:view'],
      icon: 'lucide:wallet-cards',
      order: 10,
      title: '套餐与用量',
    },
  },
];

export default routes;
