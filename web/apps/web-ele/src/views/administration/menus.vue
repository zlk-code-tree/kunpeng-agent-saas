<script lang="ts" setup>
import type { AdminMenuItem } from '#/api';

import { computed, onMounted, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElButton,
  ElCard,
  ElInput,
  ElInputNumber,
  ElMessage,
  ElSwitch,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import { listMenuSettingsApi, updateAdminMenuApi } from '#/api';

const items = ref<AdminMenuItem[]>([]);
const loading = ref(false);
const saving = ref('');
const visibleCount = computed(
  () => items.value.filter((item) => item.enabled).length,
);
const protectedCount = computed(
  () => items.value.filter((item) => item.protected).length,
);

/** 加载服务端允许管理的路由清单，不能通过管理界面增加任意前端组件。 */
async function load() {
  loading.value = true;
  try {
    items.value = await listMenuSettingsApi();
  } finally {
    loading.value = false;
  }
}

/** 保存菜单的标题、顺序与显示状态；受保护的入口由后端强制保持显示。 */
async function save(item: AdminMenuItem) {
  saving.value = item.routeName;
  try {
    await updateAdminMenuApi(item.routeName, {
      enabled: item.enabled,
      sortOrder: item.sortOrder,
      title: item.title.trim(),
    });
    ElMessage.success('菜单已保存，刷新页面后生效');
    await load();
  } finally {
    saving.value = '';
  }
}

onMounted(load);
</script>

<template>
  <Page
    class="saas-page"
    content-class="saas-page-content"
    description="调整已注册页面的菜单标题、排序与可见性；接口权限由 Gin 独立校验"
    header-class="saas-page-header"
    title="菜单管理"
  >
    <div class="saas-kpi-grid">
      <div class="saas-kpi">
        <div class="saas-kpi__label">菜单总数</div>
        <div class="saas-kpi__value">{{ items.length }}</div>
        <div class="saas-kpi__hint">已注册的前端业务入口</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">当前显示</div>
        <div class="saas-kpi__value">{{ visibleCount }}</div>
        <div class="saas-kpi__hint">刷新页面后应用最新设置</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">系统入口</div>
        <div class="saas-kpi__value">{{ protectedCount }}</div>
        <div class="saas-kpi__hint">保持后台可管理与可恢复</div>
      </div>
    </div>
    <ElCard>
      <template #header>
        <div class="saas-section-title">菜单显示与排序</div>
      </template>
      <ElTable v-loading="loading" :data="items" row-key="routeName">
        <ElTableColumn label="路由" min-width="180">
          <template #default="{ row }">
            <code
              class="rounded-md bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300"
            >
              {{ row.routeName }}
            </code>
          </template>
        </ElTableColumn>
        <ElTableColumn label="菜单标题" min-width="210">
          <template #default="{ row }">
            <ElInput v-model="row.title" maxlength="80" />
          </template>
        </ElTableColumn>
        <ElTableColumn label="排序" width="160">
          <template #default="{ row }">
            <ElInputNumber
              v-model="row.sortOrder"
              :max="1000"
              :min="-1000"
              controls-position="right"
            />
          </template>
        </ElTableColumn>
        <ElTableColumn label="显示" width="170">
          <template #default="{ row }">
            <ElTag v-if="row.protected" type="info">系统入口</ElTag>
            <ElSwitch v-else v-model="row.enabled" />
          </template>
        </ElTableColumn>
        <ElTableColumn label="操作" width="120">
          <template #default="{ row }">
            <ElButton
              :loading="saving === row.routeName"
              link
              type="primary"
              @click="save(row)"
              >保存</ElButton
            >
          </template>
        </ElTableColumn>
      </ElTable>
    </ElCard>
  </Page>
</template>
