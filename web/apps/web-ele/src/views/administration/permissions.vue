<script lang="ts" setup>
import type { PermissionItem } from '#/api';

import { computed, onMounted, ref, watch } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElButton,
  ElCard,
  ElInput,
  ElOption,
  ElPagination,
  ElSelect,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import { listPermissionsApi } from '#/api';

const loading = ref(false);
const permissions = ref<PermissionItem[]>([]);
const keyword = ref('');
const scopeFilter = ref('');
const groupFilter = ref('');
const page = ref(1);
const pageSize = ref(15);

const groups = computed(() => [
  ...new Set(permissions.value.map((item) => item.group)),
]);

const filteredPermissions = computed(() => {
  const search = keyword.value.trim().toLowerCase();
  return permissions.value.filter((item) => {
    const matchesKeyword =
      !search ||
      item.code.toLowerCase().includes(search) ||
      item.name.toLowerCase().includes(search) ||
      item.description.toLowerCase().includes(search);
    return (
      matchesKeyword &&
      (!scopeFilter.value || item.scope === scopeFilter.value) &&
      (!groupFilter.value || item.group === groupFilter.value)
    );
  });
});

const pagedPermissions = computed(() => {
  const start = (page.value - 1) * pageSize.value;
  return filteredPermissions.value.slice(start, start + pageSize.value);
});

const tenantCount = computed(
  () => permissions.value.filter((item) => item.scope === 'tenant').length,
);
const platformCount = computed(
  () => permissions.value.filter((item) => item.scope === 'platform').length,
);

watch([keyword, scopeFilter, groupFilter], () => {
  page.value = 1;
});

/** 加载权限码目录。权限码由服务端版本维护，角色页面负责权限组合与授权。 */
async function load() {
  loading.value = true;
  try {
    permissions.value = await listPermissionsApi();
  } finally {
    loading.value = false;
  }
}

function resetFilters() {
  keyword.value = '';
  scopeFilter.value = '';
  groupFilter.value = '';
}

onMounted(load);
</script>

<template>
  <Page
    class="saas-page"
    content-class="saas-page-content"
    description="查看系统中可用于接口、菜单和按钮鉴权的权限码；角色授权请在角色管理中完成"
    header-class="saas-page-header"
    title="权限管理"
  >
    <div class="saas-kpi-grid">
      <div class="saas-kpi">
        <div class="saas-kpi__label">权限节点</div>
        <div class="saas-kpi__value">{{ permissions.length }}</div>
        <div class="saas-kpi__hint">由后端统一定义</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">租户权限</div>
        <div class="saas-kpi__value">{{ tenantCount }}</div>
        <div class="saas-kpi__hint">用于空间角色授权</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">平台权限</div>
        <div class="saas-kpi__value">{{ platformCount }}</div>
        <div class="saas-kpi__hint">用于安装级后台管理</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">业务分组</div>
        <div class="saas-kpi__value">{{ groups.length }}</div>
        <div class="saas-kpi__hint">便于角色按模块授权</div>
      </div>
    </div>

    <ElCard>
      <div class="mb-5 flex flex-wrap items-center gap-3">
        <ElInput
          v-model="keyword"
          clearable
          placeholder="搜索权限名称、权限码或说明"
          style="width: 300px"
        />
        <ElSelect
          v-model="scopeFilter"
          clearable
          placeholder="权限范围"
          style="width: 150px"
        >
          <ElOption label="租户权限" value="tenant" />
          <ElOption label="平台权限" value="platform" />
        </ElSelect>
        <ElSelect
          v-model="groupFilter"
          clearable
          placeholder="业务分组"
          style="width: 160px"
        >
          <ElOption
            v-for="group in groups"
            :key="group"
            :label="group"
            :value="group"
          />
        </ElSelect>
        <ElButton
          v-if="keyword || scopeFilter || groupFilter"
          @click="resetFilters"
          >重置</ElButton
        >
        <div class="ml-auto text-sm text-gray-400">
          共 {{ filteredPermissions.length }} 条
        </div>
      </div>

      <ElTable v-loading="loading" :data="pagedPermissions" row-key="code">
        <ElTableColumn label="权限名称" min-width="160" prop="name" />
        <ElTableColumn label="权限码" min-width="210">
          <template #default="{ row }">
            <code
              class="rounded bg-gray-100 px-2 py-1 text-xs dark:bg-gray-800"
              >{{ row.code }}</code
            >
          </template>
        </ElTableColumn>
        <ElTableColumn label="范围" width="120">
          <template #default="{ row }">
            <ElTag
              :type="row.scope === 'platform' ? 'warning' : 'primary'"
              effect="plain"
            >
              {{ row.scope === 'platform' ? '平台' : '租户' }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="分组" width="140" prop="group" />
        <ElTableColumn
          label="说明"
          min-width="260"
          prop="description"
          show-overflow-tooltip
        />
      </ElTable>

      <div class="mt-5 flex justify-end">
        <ElPagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :page-sizes="[15, 30, 50]"
          :total="filteredPermissions.length"
          background
          layout="total, sizes, prev, pager, next"
        />
      </div>
    </ElCard>
  </Page>
</template>
