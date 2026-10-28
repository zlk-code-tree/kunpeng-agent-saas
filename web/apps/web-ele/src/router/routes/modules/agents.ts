import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'Agents',
    path: '/agents',
    component: () => import('#/views/agents/index.vue'),
    meta: {
      icon: 'lucide:bot',
      order: 10,
      title: '智能体中心',
    },
  },
];

export default routes;
