import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'SystemOverview',
    path: '/system',
    component: () => import('#/views/system/index.vue'),
    meta: {
      authority: ['owner', 'admin'],
      icon: 'lucide:settings-2',
      order: 30,
      title: '平台设置',
    },
  },
];

export default routes;
