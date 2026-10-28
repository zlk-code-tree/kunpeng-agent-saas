import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:message-square-more',
      order: -1,
      title: '智能体工作台',
    },
    name: 'AgentWorkspace',
    path: '/workspace',
    children: [
      {
        name: 'Workspace',
        path: '/workspace',
        component: () => import('#/views/workspace/index.vue'),
        meta: {
          affixTab: true,
          icon: 'lucide:bot-message-square',
          title: '开始对话',
        },
      },
    ],
  },
];

export default routes;
