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
const instanceForm = reactive({ apiKey: '', baseUrl: 'http://127.0.0.1:5001/v1', name: '私有 Dify' });
const bindingForm = reactive({ agentVersionId: '', apiKey: '', difyInstanceId: '', upstreamAppId: '' });
const modeText = computed(() => (info.value?.appMode === 'multi' ? '多租户 SaaS' : '单实例'));

async function load() {
  [info.value, instances.value, agents.value] = await Promise.all([
    getCapabilitiesApi(),
    listDifyInstancesApi(),
    listAgentsApi(),
  ]);
}

async function createInstance() {
  await createDifyInstanceApi(instanceForm);
  instanceForm.apiKey = '';
  instances.value = await listDifyInstancesApi();
  ElMessage.success('Dify 服务已保存，密钥已加密入库');
}

async function bindApp() {
  await bindDifyAppApi(bindingForm);
  bindingForm.apiKey = '';
  agents.value = await listAgentsApi();
  ElMessage.success('智能体应用绑定成功');
}

onMounted(load);
</script>

<template>
  <Page description="同一套代码和数据模型，通过 APP_MODE 切换单实例与多租户模式" title="平台设置">
    <div class="grid gap-5 xl:grid-cols-2">
      <ElCard>
        <ElDescriptions :column="1" border title="运行能力">
          <ElDescriptionsItem label="部署模式">
            <ElTag type="success">{{ modeText }}</ElTag>
          </ElDescriptionsItem>
          <ElDescriptionsItem label="租户边界">业务表强制 tenant_id</ElDescriptionsItem>
          <ElDescriptionsItem label="身份认证">Gin JWT + RBAC</ElDescriptionsItem>
          <ElDescriptionsItem label="智能体引擎">私有化 Dify</ElDescriptionsItem>
        </ElDescriptions>
      </ElCard>

      <ElCard header="连接私有 Dify 服务">
        <ElForm label-width="110px">
          <ElFormItem label="连接名称"><ElInput v-model="instanceForm.name" /></ElFormItem>
          <ElFormItem label="API Base URL"><ElInput v-model="instanceForm.baseUrl" /></ElFormItem>
          <ElFormItem label="Dataset Key"><ElInput v-model="instanceForm.apiKey" show-password type="password" /></ElFormItem>
          <ElFormItem><ElButton type="primary" @click="createInstance">保存连接</ElButton></ElFormItem>
        </ElForm>
      </ElCard>

      <ElCard class="xl:col-span-2" header="绑定预置智能体到 Dify 应用">
        <ElForm label-width="120px">
          <ElFormItem label="预置智能体">
            <ElSelect v-model="bindingForm.agentVersionId" class="w-full">
              <ElOption v-for="agent in agents" :key="agent.agentVersionId" :label="agent.name" :value="agent.agentVersionId" />
            </ElSelect>
          </ElFormItem>
          <ElFormItem label="Dify 服务">
            <ElSelect v-model="bindingForm.difyInstanceId" class="w-full">
              <ElOption v-for="item in instances" :key="item.id" :label="item.name" :value="item.id" />
            </ElSelect>
          </ElFormItem>
          <ElFormItem label="App API Key"><ElInput v-model="bindingForm.apiKey" show-password type="password" /></ElFormItem>
          <ElFormItem label="Dify App ID"><ElInput v-model="bindingForm.upstreamAppId" placeholder="可选，用于运维追踪" /></ElFormItem>
          <ElFormItem><ElButton type="primary" @click="bindApp">保存绑定</ElButton></ElFormItem>
        </ElForm>
      </ElCard>
    </div>
  </Page>
</template>
