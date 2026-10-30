<script lang="ts" setup>
import type { AgentItem } from '#/api';

import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';

import { Page } from '@vben/common-ui';

import { ElButton, ElCard, ElEmpty, ElTag } from 'element-plus';

import { listAgentsApi } from '#/api';

const loading = ref(false);
const agents = ref<AgentItem[]>([]);
const router = useRouter();

/** 加载当前租户已发布且可见的智能体。 */
async function loadAgents() {
  loading.value = true;
  try {
    agents.value = await listAgentsApi();
  } finally {
    loading.value = false;
  }
}

/** 将选中的智能体版本写入路由参数并进入工作台。 */
function startChat(agent: AgentItem) {
  void router.push({ path: '/workspace', query: { agentId: agent.id } });
}

onMounted(loadAgents);
</script>

<template>
  <Page
    class="saas-page"
    content-class="saas-page-content"
    description="选择平台预置智能体，并使用当前空间的参数与知识库"
    header-class="saas-page-header"
    title="智能体中心"
  >
    <div v-loading="loading" class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      <ElCard
        v-for="agent in agents"
        :key="agent.id"
        class="saas-agent-card"
        shadow="hover"
      >
        <template #header>
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-3">
              <div
                class="flex size-10 items-center justify-center rounded-xl bg-indigo-50 text-lg font-bold text-indigo-600 dark:bg-indigo-950/50 dark:text-indigo-300"
              >
                {{ agent.name.slice(0, 1) }}
              </div>
              <div>
                <strong class="block">{{ agent.name }}</strong>
                <span class="mt-1 block text-xs text-gray-400">{{
                  agent.category || '通用智能体'
                }}</span>
              </div>
            </div>
            <ElTag effect="plain">v{{ agent.version }}</ElTag>
          </div>
        </template>
        <p class="mb-5 min-h-12 text-sm text-gray-500">
          {{ agent.description || '暂无介绍' }}
        </p>
        <ElButton
          class="w-full"
          :disabled="!agent.enabled || !agent.configured"
          type="primary"
          @click="startChat(agent)"
        >
          {{
            !agent.enabled
              ? '暂未启用'
              : agent.configured
                ? '开始使用'
                : '等待绑定 Dify'
          }}
        </ElButton>
      </ElCard>
    </div>
    <ElEmpty
      v-if="!loading && agents.length === 0"
      description="管理员尚未发布智能体"
    />
  </Page>
</template>
