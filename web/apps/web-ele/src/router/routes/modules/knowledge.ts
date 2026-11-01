import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'KnowledgeBases',
    path: '/knowledge',
    component: () => import('#/views/knowledge/index.vue'),
    meta: {
      authority: ['knowledge:view'],
      icon: 'lucide:library-big',
      order: 20,
      title: '私有知识库',
    },
  },
];

export default routes;
