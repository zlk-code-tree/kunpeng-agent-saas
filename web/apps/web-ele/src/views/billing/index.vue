<script lang="ts" setup>
import type {
  BillingAccount,
  BillingLedgerEntry,
  BillingPlan,
  BillingSubscription,
} from '#/api';

import { computed, onMounted, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElCard,
  ElDescriptions,
  ElDescriptionsItem,
  ElProgress,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import {
  getBillingAccountApi,
  getBillingSubscriptionApi,
  listBillingLedgerApi,
  listBillingPlansApi,
} from '#/api';

const loading = ref(false);
const account = ref<BillingAccount>();
const subscription = ref<BillingSubscription>();
const plans = ref<BillingPlan[]>([]);
const ledger = ref<BillingLedgerEntry[]>([]);

const benefitLabels: Record<string, string> = {
  agent_calls: '智能体调用次数',
  documents: '知识库文档数',
  knowledge_storage_mb: '知识库存储（MB）',
  members: '成员数',
  token_credits: 'Token 额度',
};

const available = computed(() => Number(account.value?.available || 0));

/** 按中文数字格式显示余额和额度，最多保留四位小数。 */
function formatNumber(value: string | number) {
  return Number(value).toLocaleString('zh-CN', { maximumFractionDigits: 4 });
}

/** 将后端钱包流水类型转换为用户可读文案。 */
function entryLabel(type: string) {
  return (
    {
      adjust: '人工调整',
      consume: '用量结算',
      grant: '额度发放',
      recharge: '充值',
      refund: '退款',
      release: '释放预占',
      reserve: '额度预占',
    }[type] || type
  );
}

/** 计算权益使用百分比，并限制在进度条允许的 0～100 范围内。 */
function entitlementPercent(used: string, limit: string) {
  const maximum = Number(limit);
  if (maximum <= 0) return 0;
  return Math.min(100, Math.round((Number(used) / maximum) * 100));
}

/** 并行加载计费首页所需的账户、订阅、套餐和流水。 */
async function load() {
  loading.value = true;
  try {
    [account.value, subscription.value, plans.value, ledger.value] =
      await Promise.all([
        getBillingAccountApi(),
        getBillingSubscriptionApi(),
        listBillingPlansApi(),
        listBillingLedgerApi(),
      ]);
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <Page
    class="saas-page"
    content-class="saas-page-content"
    description="额度采用预占、结算、释放三段式记账，所有变化均保留不可变流水"
    header-class="saas-page-header"
    title="套餐与用量"
  >
    <div v-loading="loading" class="grid gap-4 lg:grid-cols-3">
      <ElCard class="lg:col-span-1" header="可用额度" shadow="never">
        <div class="text-4xl font-semibold">{{ formatNumber(available) }}</div>
        <div class="mt-2 text-sm text-gray-500">单位：Credit</div>
        <ElDescriptions class="mt-5" :column="1" border>
          <ElDescriptionsItem label="账户余额">
            {{ formatNumber(account?.balance || 0) }}
          </ElDescriptionsItem>
          <ElDescriptionsItem label="预占额度">
            {{ formatNumber(account?.frozenAmount || 0) }}
          </ElDescriptionsItem>
          <ElDescriptionsItem label="计费主体">
            {{ account?.subjectType === 'tenant' ? '企业空间' : '个人用户' }}
          </ElDescriptionsItem>
        </ElDescriptions>
      </ElCard>

      <ElCard class="lg:col-span-2" shadow="never">
        <template #header>
          <div class="flex items-center justify-between">
            <strong>当前套餐</strong>
            <ElTag type="success">{{ subscription?.status || '-' }}</ElTag>
          </div>
        </template>
        <div class="mb-4 flex items-end justify-between">
          <div>
            <div class="text-2xl font-semibold">
              {{ subscription?.planName }}
            </div>
            <div class="mt-1 text-sm text-gray-500">
              周期截止：{{
                subscription
                  ? new Date(subscription.currentPeriodEnd).toLocaleString()
                  : '-'
              }}
            </div>
          </div>
        </div>
        <div class="grid gap-4 md:grid-cols-2">
          <div
            v-for="item in subscription?.entitlements || []"
            :key="item.key"
            class="rounded-lg border p-3"
          >
            <div class="mb-2 flex justify-between text-sm">
              <span>{{ benefitLabels[item.key] || item.key }}</span>
              <span
                >{{ formatNumber(item.used) }} /
                {{ formatNumber(item.limit) }}</span
              >
            </div>
            <ElProgress
              :percentage="entitlementPercent(item.used, item.limit)"
              :show-text="false"
            />
          </div>
        </div>
      </ElCard>
    </div>

    <div class="mt-5 grid gap-4 lg:grid-cols-3">
      <ElCard v-for="plan in plans" :key="plan.versionId" shadow="hover">
        <template #header>
          <div class="flex items-center justify-between">
            <strong>{{ plan.name }}</strong>
            <ElTag v-if="plan.code === subscription?.planCode" type="success"
              >当前套餐</ElTag
            >
          </div>
        </template>
        <div class="mb-4 text-3xl font-semibold">
          ¥{{ formatNumber(plan.price) }}
          <span class="text-sm font-normal text-gray-500"
            >/{{ plan.billingPeriod === 'year' ? '年' : '月' }}</span
          >
        </div>
        <div
          v-for="(value, key) in plan.benefits"
          :key="key"
          class="mb-2 text-sm"
        >
          {{ benefitLabels[key] || key }}：{{ formatNumber(value) }}
        </div>
      </ElCard>
    </div>

    <ElCard class="mt-5" header="额度流水" shadow="never">
      <ElTable :data="ledger">
        <ElTableColumn label="时间" width="190">
          <template #default="{ row }">{{
            new Date(row.createdAt).toLocaleString()
          }}</template>
        </ElTableColumn>
        <ElTableColumn label="类型" width="130">
          <template #default="{ row }">{{
            entryLabel(row.entryType)
          }}</template>
        </ElTableColumn>
        <ElTableColumn label="变动" width="130">
          <template #default="{ row }">
            <span
              :class="
                Number(row.amount) >= 0 ? 'text-green-600' : 'text-red-600'
              "
            >
              {{ Number(row.amount) > 0 ? '+' : ''
              }}{{ formatNumber(row.amount) }}
            </span>
          </template>
        </ElTableColumn>
        <ElTableColumn label="余额" prop="balanceAfter" width="140">
          <template #default="{ row }">{{
            formatNumber(row.balanceAfter)
          }}</template>
        </ElTableColumn>
        <ElTableColumn label="预占" prop="frozenAfter" width="140">
          <template #default="{ row }">{{
            formatNumber(row.frozenAfter)
          }}</template>
        </ElTableColumn>
        <ElTableColumn label="业务" min-width="180" prop="businessType" />
      </ElTable>
    </ElCard>
  </Page>
</template>
