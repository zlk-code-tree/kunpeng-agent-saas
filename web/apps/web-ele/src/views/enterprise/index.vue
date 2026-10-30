<script lang="ts" setup>
import type {
  CostSummaryItem,
  DifyInstanceItem,
  ModelProviderItem,
  ReconciliationReport,
  ToolItem,
} from '#/api';

import { onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElButton,
  ElCard,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElOption,
  ElSelect,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import {
  checkDifyInstanceApi,
  createModelProviderApi,
  createToolApi,
  getCostSummaryApi,
  listDifyInstancesApi,
  listModelProvidersApi,
  listToolsApi,
  reconcileBillingApi,
  setModelPriceApi,
} from '#/api';

const loading = ref(false);
const instances = ref<DifyInstanceItem[]>([]);
const providers = ref<ModelProviderItem[]>([]);
const tools = ref<ToolItem[]>([]);
const costs = ref<CostSummaryItem[]>([]);
const reconciliation = ref<ReconciliationReport>();
const providerVisible = ref(false);
const priceVisible = ref(false);
const toolVisible = ref(false);
const selectedProvider = ref<ModelProviderItem>();
const providerForm = reactive({
  apiKey: '',
  baseUrl: 'http://127.0.0.1:11434/v1',
  code: 'ollama-local',
  name: '本地 Ollama',
  providerType: 'ollama',
});
const priceForm = reactive({
  currency: 'CNY',
  inputPer1000: '0',
  model: '',
  outputPer1000: '0',
});
const toolForm = reactive<{
  code: string;
  credential: string;
  description: string;
  endpoint: string;
  name: string;
  protocol: 'http_json' | 'mcp_http';
}>({
  code: '',
  credential: '',
  description: '',
  endpoint: '',
  name: '',
  protocol: 'mcp_http',
});

/** 把集成健康状态映射为 Element Plus 标签颜色。 */
function healthType(status: string) {
  if (status === 'healthy') return 'success';
  if (status === 'unavailable') return 'danger';
  if (status === 'degraded') return 'warning';
  return 'info';
}

/** 并行加载企业集成、工具和成本数据。 */
async function load() {
  loading.value = true;
  try {
    [instances.value, providers.value, tools.value, costs.value] =
      await Promise.all([
        listDifyInstancesApi(),
        listModelProvidersApi(),
        listToolsApi(),
        getCostSummaryApi(),
      ]);
  } finally {
    loading.value = false;
  }
}

/** 立即检查选中 Dify 实例并刷新列表。 */
async function checkInstance(item: DifyInstanceItem) {
  await checkDifyInstanceApi(item.id);
  ElMessage.success('健康检查已完成');
  await load();
}

/** 保存模型供应商并刷新页面数据。 */
async function createProvider() {
  await createModelProviderApi(providerForm);
  providerVisible.value = false;
  ElMessage.success('模型供应商已保存');
  await load();
}

/** 记录当前供应商并打开价格版本发布弹窗。 */
function openPrice(item: ModelProviderItem) {
  selectedProvider.value = item;
  priceVisible.value = true;
}

/** 为当前供应商发布模型成本价格。 */
async function savePrice() {
  if (!selectedProvider.value) return;
  await setModelPriceApi(selectedProvider.value.id, priceForm);
  priceVisible.value = false;
  ElMessage.success('模型价格版本已发布');
}

/** 创建受控工具并刷新工具目录。 */
async function createTool() {
  await createToolApi(toolForm);
  toolVisible.value = false;
  ElMessage.success('工具版本已发布');
  await load();
}

/** 执行对账扫描并展示本次异常统计。 */
async function reconcile() {
  reconciliation.value = await reconcileBillingApi();
  ElMessage.success('对账扫描完成');
}

onMounted(load);
</script>

<template>
  <Page
    class="saas-page"
    content-class="saas-page-content"
    description="管理企业独立 Dify、本地模型、MCP 工具、成本价格与账务对账"
    header-class="saas-page-header"
    title="企业集成与成本"
  >
    <template #extra>
      <ElButton @click="reconcile">执行对账</ElButton>
      <ElButton @click="toolVisible = true">新建工具</ElButton>
      <ElButton type="primary" @click="providerVisible = true"
        >新建模型供应商</ElButton
      >
    </template>

    <div v-loading="loading" class="grid gap-5">
      <ElCard header="Dify 实例健康与隔离" shadow="never">
        <ElTable :data="instances">
          <ElTableColumn label="实例" min-width="180" prop="name" />
          <ElTableColumn label="部署" width="120" prop="deploymentType" />
          <ElTableColumn label="健康" width="130">
            <template #default="{ row }"
              ><ElTag :type="healthType(row.healthStatus)">{{
                row.healthStatus
              }}</ElTag></template
            >
          </ElTableColumn>
          <ElTableColumn
            label="连续失败"
            width="100"
            prop="consecutiveFailures"
          />
          <ElTableColumn
            label="凭据版本"
            width="100"
            prop="credentialVersion"
          />
          <ElTableColumn label="操作" width="120">
            <template #default="{ row }"
              ><ElButton link type="primary" @click="checkInstance(row)"
                >立即检查</ElButton
              ></template
            >
          </ElTableColumn>
        </ElTable>
      </ElCard>

      <ElCard header="模型供应商与价格" shadow="never">
        <ElTable :data="providers">
          <ElTableColumn label="名称" prop="name" />
          <ElTableColumn label="类型" width="120" prop="providerType" />
          <ElTableColumn label="地址" min-width="220" prop="baseUrl" />
          <ElTableColumn label="健康" width="120" prop="healthStatus" />
          <ElTableColumn label="操作" width="120">
            <template #default="{ row }"
              ><ElButton link type="primary" @click="openPrice(row)"
                >发布价格</ElButton
              ></template
            >
          </ElTableColumn>
        </ElTable>
      </ElCard>

      <ElCard header="HTTP / MCP 工具目录" shadow="never">
        <ElTable :data="tools">
          <ElTableColumn label="工具" prop="name" />
          <ElTableColumn label="编码" prop="code" />
          <ElTableColumn label="协议" width="120" prop="protocol" />
          <ElTableColumn label="版本" width="90" prop="version" />
          <ElTableColumn label="端点" min-width="240" prop="endpoint" />
        </ElTable>
      </ElCard>

      <ElCard header="供应商成本汇总" shadow="never">
        <ElTable :data="costs">
          <ElTableColumn label="成本类型" prop="costType" />
          <ElTableColumn label="供应商" prop="provider" />
          <ElTableColumn label="模型" prop="model" />
          <ElTableColumn label="金额">
            <template #default="{ row }"
              >{{ Number(row.amount).toFixed(6) }} {{ row.currency }}</template
            >
          </ElTableColumn>
        </ElTable>
      </ElCard>

      <ElCard v-if="reconciliation" header="最近对账结果" shadow="never">
        <div class="grid gap-3 md:grid-cols-4">
          <div>
            成功但无用量：{{ reconciliation.succeededRunsMissingUsage }}
          </div>
          <div>等待用量：{{ reconciliation.reservationsAwaitingUsage }}</div>
          <div>待人工核对：{{ reconciliation.reservationsInReview }}</div>
          <div>缺少成本：{{ reconciliation.usageMissingCost }}</div>
        </div>
      </ElCard>
    </div>

    <ElDialog v-model="providerVisible" title="新建模型供应商" width="560px">
      <ElForm label-width="100px">
        <ElFormItem label="编码"
          ><ElInput v-model="providerForm.code"
        /></ElFormItem>
        <ElFormItem label="名称"
          ><ElInput v-model="providerForm.name"
        /></ElFormItem>
        <ElFormItem label="类型"
          ><ElSelect v-model="providerForm.providerType" class="w-full"
            ><ElOption label="OpenAI" value="openai" /><ElOption
              label="DeepSeek"
              value="deepseek" /><ElOption
              label="Ollama"
              value="ollama" /><ElOption label="vLLM" value="vllm" /><ElOption
              label="兼容接口"
              value="compatible" /></ElSelect
        ></ElFormItem>
        <ElFormItem label="接口地址"
          ><ElInput v-model="providerForm.baseUrl"
        /></ElFormItem>
        <ElFormItem label="API Key"
          ><ElInput v-model="providerForm.apiKey" show-password type="password"
        /></ElFormItem>
      </ElForm>
      <template #footer
        ><ElButton @click="providerVisible = false">取消</ElButton
        ><ElButton type="primary" @click="createProvider"
          >保存</ElButton
        ></template
      >
    </ElDialog>

    <ElDialog v-model="priceVisible" title="发布模型成本价格" width="520px">
      <ElForm label-width="120px">
        <ElFormItem label="模型"
          ><ElInput v-model="priceForm.model"
        /></ElFormItem>
        <ElFormItem label="输入/千 Token"
          ><ElInput v-model="priceForm.inputPer1000"
        /></ElFormItem>
        <ElFormItem label="输出/千 Token"
          ><ElInput v-model="priceForm.outputPer1000"
        /></ElFormItem>
        <ElFormItem label="币种"
          ><ElInput v-model="priceForm.currency"
        /></ElFormItem>
      </ElForm>
      <template #footer
        ><ElButton @click="priceVisible = false">取消</ElButton
        ><ElButton type="primary" @click="savePrice">发布</ElButton></template
      >
    </ElDialog>

    <ElDialog v-model="toolVisible" title="新建受控工具" width="560px">
      <ElForm label-width="100px">
        <ElFormItem label="编码"
          ><ElInput v-model="toolForm.code"
        /></ElFormItem>
        <ElFormItem label="名称"
          ><ElInput v-model="toolForm.name"
        /></ElFormItem>
        <ElFormItem label="协议"
          ><ElSelect v-model="toolForm.protocol" class="w-full"
            ><ElOption label="MCP HTTP" value="mcp_http" /><ElOption
              label="HTTP JSON"
              value="http_json" /></ElSelect
        ></ElFormItem>
        <ElFormItem label="端点"
          ><ElInput v-model="toolForm.endpoint"
        /></ElFormItem>
        <ElFormItem label="凭据"
          ><ElInput v-model="toolForm.credential" show-password type="password"
        /></ElFormItem>
        <ElFormItem label="说明"
          ><ElInput v-model="toolForm.description" type="textarea"
        /></ElFormItem>
      </ElForm>
      <template #footer
        ><ElButton @click="toolVisible = false">取消</ElButton
        ><ElButton type="primary" @click="createTool">发布</ElButton></template
      >
    </ElDialog>
  </Page>
</template>
