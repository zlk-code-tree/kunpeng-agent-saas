<script lang="ts" setup>
import type { AgentItem, CapabilityInfo, DifyInstanceItem } from '#/api';

import { computed, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElButton,
  ElCard,
  ElDescriptions,
  ElDescriptionsItem,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElOption,
  ElSelect,
  ElTag,
} from 'element-plus';

import {
  bindDifyAppApi,
  createDifyInstanceApi,
  getCapabilitiesApi,
  listAgentsApi,
  listDifyInstancesApi,
} from '#/api';

const info = ref<CapabilityInfo>();
const instances = ref<DifyInstanceItem[]>([]);
const agents = ref<AgentItem[]>([]);
const instanceForm = reactive({
  apiKey: '',
  baseUrl: 'http://127.0.0.1:5001/v1',
  name: '私有 Dify',
});
const bindingForm = reactive({
  agentVersionId: '',
  apiKey: '',
  difyInstanceId: '',
  upstreamAppId: '',
});
const modeText = computed(() =>
  info.value?.appMode === 'multi' ? '多租户 SaaS' : '单实例',
);
const configuredAgents = computed(
  () => agents.value.filter((item) => item.configured).length,
);
const healthyInstances = computed(
  () =>
    instances.value.filter((item) => item.healthStatus === 'healthy').length,
);

/** 并行加载智能体和 Dify 实例供绑定配置使用。 */
async function load() {
  [info.value, instances.value, agents.value] = await Promise.all([
    getCapabilitiesApi(),
    listDifyInstancesApi(),
    listAgentsApi(),
  ]);
}

/** 创建 Dify 实例并刷新候选列表。 */
async function createInstance() {
  await createDifyInstanceApi(instanceForm);
  instanceForm.apiKey = '';
  instances.value = await listDifyInstancesApi();
  ElMessage.success('Dify 服务已保存，密钥已加密入库');
}

/** 把选中的智能体版本绑定到选中的 Dify 实例。 */
async function bindApp() {
  await bindDifyAppApi(bindingForm);
  bindingForm.apiKey = '';
  agents.value = await listAgentsApi();
  ElMessage.success('智能体应用绑定成功');
}

onMounted(load);
</script>

<template>
  <Page
    class="saas-page"
    content-class="saas-page-content"
    description="同一套代码和数据模型，通过 APP_MODE 切换单实例与多租户模式"
    header-class="saas-page-header"
    title="平台设置"
  >
    <div class="saas-kpi-grid">
      <div class="saas-kpi">
        <div class="saas-kpi__label">部署模式</div>
        <div class="saas-kpi__value !text-2xl">{{ modeText }}</div>
        <div class="saas-kpi__hint">由 APP_MODE 统一控制</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">Dify 实例</div>
        <div class="saas-kpi__value">{{ instances.length }}</div>
        <div class="saas-kpi__hint">{{ healthyInstances }} 个实例运行健康</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">已绑定智能体</div>
        <div class="saas-kpi__value">{{ configuredAgents }}</div>
        <div class="saas-kpi__hint">共 {{ agents.length }} 个预置版本</div>
      </div>
    </div>
    <div class="grid gap-5 xl:grid-cols-2">
      <ElCard>
        <template #header>
          <div class="saas-section-title">运行能力</div>
        </template>
        <ElDescriptions :column="1" border>
          <ElDescriptionsItem label="部署模式">
            <ElTag type="success">{{ modeText }}</ElTag>
          </ElDescriptionsItem>
          <ElDescriptionsItem label="租户边界"
            >业务表强制 tenant_id</ElDescriptionsItem
          >
          <ElDescriptionsItem label="身份认证"
            >Gin JWT + RBAC</ElDescriptionsItem
          >
          <ElDescriptionsItem label="智能体引擎"
            >私有化 Dify</ElDescriptionsItem
          >
        </ElDescriptions>
      </ElCard>

      <ElCard>
        <template #header>
          <div class="saas-section-title">连接私有 Dify 服务</div>
        </template>
        <ElForm label-width="110px">
          <ElFormItem label="连接名称"
            ><ElInput v-model="instanceForm.name"
          /></ElFormItem>
          <ElFormItem label="API Base URL"
            ><ElInput v-model="instanceForm.baseUrl"
          /></ElFormItem>
          <ElFormItem label="Dataset Key"
            ><ElInput
              v-model="instanceForm.apiKey"
              show-password
              type="password"
          /></ElFormItem>
          <ElFormItem class="!mb-0"
            ><ElButton type="primary" @click="createInstance"
              >保存连接</ElButton
            ></ElFormItem
          >
        </ElForm>
      </ElCard>

      <ElCard class="xl:col-span-2">
        <template #header>
          <div class="saas-section-title">绑定预置智能体到 Dify 应用</div>
        </template>
        <ElForm label-width="120px">
          <ElFormItem label="预置智能体">
            <ElSelect v-model="bindingForm.agentVersionId" class="w-full">
              <ElOption
                v-for="agent in agents"
                :key="agent.agentVersionId"
                :label="agent.name"
                :value="agent.agentVersionId"
              />
            </ElSelect>
          </ElFormItem>
          <ElFormItem label="Dify 服务">
            <ElSelect v-model="bindingForm.difyInstanceId" class="w-full">
              <ElOption
                v-for="item in instances"
                :key="item.id"
                :label="item.name"
                :value="item.id"
              />
            </ElSelect>
          </ElFormItem>
          <ElFormItem label="App API Key"
            ><ElInput
              v-model="bindingForm.apiKey"
              show-password
              type="password"
          /></ElFormItem>
          <ElFormItem label="Dify App ID"
            ><ElInput
              v-model="bindingForm.upstreamAppId"
              placeholder="可选，用于运维追踪"
          /></ElFormItem>
          <ElFormItem class="!mb-0"
            ><ElButton type="primary" @click="bindApp"
              >保存绑定</ElButton
            ></ElFormItem
          >
        </ElForm>
      </ElCard>
    </div>
  </Page>
</template>
