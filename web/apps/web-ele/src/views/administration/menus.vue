<script lang="ts" setup>
import type { AdminMenuItem } from '#/api';

import { computed, onMounted, reactive, ref, watch } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElButton,
  ElCard,
  ElDrawer,
  ElForm,
  ElFormItem,
  ElInput,
  ElInputNumber,
  ElLink,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElPagination,
  ElSelect,
  ElSwitch,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import {
  createAdminMenuApi,
  deleteAdminMenuApi,
  listMenuSettingsApi,
  updateAdminMenuApi,
} from '#/api';

const items = ref<AdminMenuItem[]>([]);
const loading = ref(false);
const submitting = ref(false);
const keyword = ref('');
const typeFilter = ref('');
const statusFilter = ref('');
const page = ref(1);
const pageSize = ref(10);
const drawerVisible = ref(false);
const editingItem = ref<AdminMenuItem>();

const form = reactive({
  enabled: true,
  icon: '',
  linkUrl: '',
  openMode: 'window' as 'iframe' | 'window',
  sortOrder: 50,
  title: '',
});

const filteredItems = computed(() => {
  const search = keyword.value.trim().toLowerCase();
  return items.value.filter((item) => {
    const matchesKeyword =
      !search ||
      item.title.toLowerCase().includes(search) ||
      item.routeName.toLowerCase().includes(search) ||
      item.linkUrl.toLowerCase().includes(search);
    const matchesType = !typeFilter.value || item.menuType === typeFilter.value;
    const matchesStatus =
      !statusFilter.value ||
      (statusFilter.value === 'enabled' ? item.enabled : !item.enabled);
    return matchesKeyword && matchesType && matchesStatus;
  });
});

const pagedItems = computed(() => {
  const start = (page.value - 1) * pageSize.value;
  return filteredItems.value.slice(start, start + pageSize.value);
});
const visibleCount = computed(
  () => items.value.filter((item) => item.enabled).length,
);
const customCount = computed(
  () => items.value.filter((item) => item.menuType === 'link').length,
);
const hiddenCount = computed(() => items.value.length - visibleCount.value);

watch([keyword, typeFilter, statusFilter], () => {
  page.value = 1;
});

/** 加载所有已注册的内置菜单及管理员创建的自定义入口。 */
async function load() {
  loading.value = true;
  try {
    items.value = await listMenuSettingsApi();
  } finally {
    loading.value = false;
  }
}

function resetForm() {
  Object.assign(form, {
    enabled: true,
    icon: '',
    linkUrl: '',
    openMode: 'window',
    sortOrder: 50,
    title: '',
  });
}

/** 新增项仅允许安全的外链或内嵌页，不能向数据库注入前端组件。 */
function openCreate() {
  editingItem.value = undefined;
  resetForm();
  drawerVisible.value = true;
}

/** 内置菜单允许修改名称、排序和显隐；自定义菜单还可修改链接与图标。 */
function openEdit(item: AdminMenuItem) {
  editingItem.value = item;
  Object.assign(form, {
    enabled: item.enabled,
    icon: item.icon,
    linkUrl: item.linkUrl,
    openMode: item.openMode,
    sortOrder: item.sortOrder,
    title: item.title,
  });
  drawerVisible.value = true;
}

/** 保存菜单设置，服务端会再次校验 URL 协议和系统菜单保护规则。 */
async function submit() {
  if (!form.title.trim()) {
    ElMessage.warning('请填写菜单名称');
    return;
  }
  const custom = !editingItem.value || editingItem.value.menuType === 'link';
  if (custom && !/^https?:\/\/\S+/u.test(form.linkUrl.trim())) {
    ElMessage.warning('链接地址必须以 http:// 或 https:// 开头');
    return;
  }

  submitting.value = true;
  try {
    if (editingItem.value) {
      await updateAdminMenuApi(editingItem.value.routeName, {
        enabled: editingItem.value.protected ? true : form.enabled,
        icon: form.icon.trim(),
        linkUrl: form.linkUrl.trim(),
        openMode: form.openMode,
        sortOrder: form.sortOrder,
        title: form.title.trim(),
      });
      ElMessage.success('菜单设置已保存，重新登录后应用到侧边栏');
    } else {
      await createAdminMenuApi({
        enabled: form.enabled,
        icon: form.icon.trim(),
        linkUrl: form.linkUrl.trim(),
        openMode: form.openMode,
        sortOrder: form.sortOrder,
        title: form.title.trim(),
      });
      ElMessage.success('自定义菜单已创建，重新登录后显示');
    }
    drawerVisible.value = false;
    await load();
  } finally {
    submitting.value = false;
  }
}

/** 删除自定义菜单；内置路由必须随代码发布，不能在后台删除。 */
async function remove(item: AdminMenuItem) {
  await ElMessageBox.confirm(
    `确定删除自定义菜单「${item.title}」吗？`,
    '删除菜单',
    { confirmButtonText: '确认删除', type: 'warning' },
  );
  await deleteAdminMenuApi(item.routeName);
  ElMessage.success('自定义菜单已删除');
  await load();
}

function resetFilters() {
  keyword.value = '';
  typeFilter.value = '';
  statusFilter.value = '';
}

onMounted(load);
</script>

<template>
  <Page
    class="saas-page"
    content-class="saas-page-content"
    description="维护侧边栏菜单名称、排序和显隐，也可添加安全的外链或内嵌业务页面"
    header-class="saas-page-header"
    title="菜单管理"
  >
    <template #extra>
      <ElButton
        v-access:code="'menu:manage'"
        type="primary"
        @click="openCreate"
      >
        新增自定义菜单
      </ElButton>
    </template>

    <div class="saas-kpi-grid">
      <div class="saas-kpi">
        <div class="saas-kpi__label">菜单总数</div>
        <div class="saas-kpi__value">{{ items.length }}</div>
        <div class="saas-kpi__hint">内置与自定义入口</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">当前显示</div>
        <div class="saas-kpi__value">{{ visibleCount }}</div>
        <div class="saas-kpi__hint">登录后可见</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">已隐藏</div>
        <div class="saas-kpi__value">{{ hiddenCount }}</div>
        <div class="saas-kpi__hint">保留路由但不显示</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">自定义菜单</div>
        <div class="saas-kpi__value">{{ customCount }}</div>
        <div class="saas-kpi__hint">外链或内嵌页面</div>
      </div>
    </div>

    <ElCard>
      <div class="mb-5 flex flex-wrap items-center gap-3">
        <ElInput
          v-model="keyword"
          clearable
          placeholder="搜索菜单名称、路由或链接"
          style="width: 300px"
        />
        <ElSelect
          v-model="typeFilter"
          clearable
          placeholder="菜单类型"
          style="width: 150px"
        >
          <ElOption label="内置菜单" value="builtin" />
          <ElOption label="自定义菜单" value="link" />
        </ElSelect>
        <ElSelect
          v-model="statusFilter"
          clearable
          placeholder="显示状态"
          style="width: 140px"
        >
          <ElOption label="显示" value="enabled" />
          <ElOption label="隐藏" value="disabled" />
        </ElSelect>
        <ElButton
          v-if="keyword || typeFilter || statusFilter"
          @click="resetFilters"
          >重置</ElButton
        >
        <div class="ml-auto text-sm text-gray-400">
          共 {{ filteredItems.length }} 条
        </div>
      </div>

      <ElTable v-loading="loading" :data="pagedItems" row-key="routeName">
        <ElTableColumn label="菜单名称" min-width="180">
          <template #default="{ row }">
            <div class="font-medium">{{ row.title }}</div>
            <code class="mt-1 inline-block text-xs text-gray-400">{{
              row.routeName
            }}</code>
          </template>
        </ElTableColumn>
        <ElTableColumn label="类型" width="120">
          <template #default="{ row }">
            <ElTag
              :type="row.menuType === 'link' ? 'success' : 'info'"
              effect="plain"
            >
              {{ row.menuType === 'link' ? '自定义' : '内置' }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="目标" min-width="240">
          <template #default="{ row }">
            <template v-if="row.menuType === 'link'">
              <ElLink :href="row.linkUrl" target="_blank" type="primary">{{
                row.linkUrl
              }}</ElLink>
              <div class="mt-1 text-xs text-gray-400">
                {{ row.openMode === 'iframe' ? '站内内嵌' : '新窗口打开' }}
              </div>
            </template>
            <span v-else class="text-sm text-gray-400">前端注册路由</span>
          </template>
        </ElTableColumn>
        <ElTableColumn
          align="center"
          label="排序"
          width="90"
          prop="sortOrder"
        />
        <ElTableColumn label="状态" width="120">
          <template #default="{ row }">
            <ElTag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? '显示' : '隐藏' }}
            </ElTag>
            <div v-if="row.protected" class="mt-1 text-xs text-gray-400">
              系统保护
            </div>
          </template>
        </ElTableColumn>
        <ElTableColumn fixed="right" label="操作" width="150">
          <template #default="{ row }">
            <ElButton link type="primary" @click="openEdit(row)">编辑</ElButton>
            <ElButton
              v-if="row.menuType === 'link'"
              link
              type="danger"
              @click="remove(row)"
            >
              删除
            </ElButton>
          </template>
        </ElTableColumn>
      </ElTable>

      <div class="mt-5 flex justify-end">
        <ElPagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50]"
          :total="filteredItems.length"
          background
          layout="total, sizes, prev, pager, next"
        />
      </div>
    </ElCard>

    <ElDrawer
      v-model="drawerVisible"
      :title="editingItem ? '编辑菜单' : '新增自定义菜单'"
      destroy-on-close
      size="520px"
    >
      <ElForm label-position="top">
        <ElFormItem label="菜单名称" required>
          <ElInput
            v-model="form.title"
            maxlength="80"
            placeholder="显示在侧边栏中的名称"
          />
        </ElFormItem>
        <template v-if="!editingItem || editingItem.menuType === 'link'">
          <ElFormItem label="链接地址" required>
            <ElInput v-model="form.linkUrl" placeholder="https://example.com" />
          </ElFormItem>
          <ElFormItem label="打开方式" required>
            <ElSelect v-model="form.openMode" class="w-full">
              <ElOption label="新窗口打开" value="window" />
              <ElOption label="站内内嵌页面" value="iframe" />
            </ElSelect>
          </ElFormItem>
          <ElFormItem label="菜单图标">
            <ElInput
              v-model="form.icon"
              placeholder="例如 lucide:link；留空使用默认图标"
            />
          </ElFormItem>
        </template>
        <ElFormItem label="菜单排序" required>
          <ElInputNumber
            v-model="form.sortOrder"
            :max="1000"
            :min="-1000"
            controls-position="right"
          />
          <span class="ml-3 text-xs text-gray-400">数值越小越靠前</span>
        </ElFormItem>
        <ElFormItem label="显示状态">
          <div class="flex items-center gap-3">
            <ElSwitch
              v-model="form.enabled"
              :disabled="editingItem?.protected"
            />
            <span class="text-sm text-gray-500">
              {{
                editingItem?.protected
                  ? '系统保护菜单不能隐藏'
                  : '关闭后侧边栏不再显示'
              }}
            </span>
          </div>
        </ElFormItem>
      </ElForm>
      <template #footer>
        <div class="flex justify-end gap-2">
          <ElButton @click="drawerVisible = false">取消</ElButton>
          <ElButton :loading="submitting" type="primary" @click="submit"
            >保存</ElButton
          >
        </div>
      </template>
    </ElDrawer>
  </Page>
</template>
