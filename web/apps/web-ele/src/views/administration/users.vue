<script lang="ts" setup>
import type { PlatformUserItem } from '#/api';

import { computed, onMounted, reactive, ref, watch } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElButton,
  ElCard,
  ElDrawer,
  ElForm,
  ElFormItem,
  ElInput,
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
  createPlatformUserApi,
  listPlatformUsersApi,
  setPlatformAdminApi,
  updatePlatformUserApi,
} from '#/api';

type UserStatus = 'active' | 'disabled';

const loading = ref(false);
const submitting = ref(false);
const users = ref<PlatformUserItem[]>([]);
const keyword = ref('');
const statusFilter = ref('');
const roleFilter = ref('');
const page = ref(1);
const pageSize = ref(10);
const drawerVisible = ref(false);
const editingUser = ref<PlatformUserItem>();

const form = reactive({
  displayName: '',
  isPlatformAdmin: false,
  password: '',
  status: 'active' as UserStatus,
  username: '',
});

const filteredUsers = computed(() => {
  const search = keyword.value.trim().toLowerCase();
  return users.value.filter((item) => {
    const matchesKeyword =
      !search ||
      item.username.toLowerCase().includes(search) ||
      item.displayName.toLowerCase().includes(search);
    const matchesStatus =
      !statusFilter.value || item.status === statusFilter.value;
    const matchesRole =
      !roleFilter.value ||
      (roleFilter.value === 'admin'
        ? item.isPlatformAdmin
        : !item.isPlatformAdmin);
    return matchesKeyword && matchesStatus && matchesRole;
  });
});

const pagedUsers = computed(() => {
  const start = (page.value - 1) * pageSize.value;
  return filteredUsers.value.slice(start, start + pageSize.value);
});

const activeCount = computed(
  () => users.value.filter((item) => item.status === 'active').length,
);
const adminCount = computed(
  () => users.value.filter((item) => item.isPlatformAdmin).length,
);
const disabledCount = computed(
  () => users.value.filter((item) => item.status !== 'active').length,
);

watch([keyword, statusFilter, roleFilter], () => {
  page.value = 1;
});

/** 加载安装级用户清单。租户成员在“空间与成员”页面单独管理。 */
async function load() {
  loading.value = true;
  try {
    users.value = await listPlatformUsersApi();
  } finally {
    loading.value = false;
  }
}

/** 打开新增用户抽屉并恢复默认值。 */
function openCreate() {
  editingUser.value = undefined;
  Object.assign(form, {
    displayName: '',
    isPlatformAdmin: false,
    password: '',
    status: 'active',
    username: '',
  });
  drawerVisible.value = true;
}

/** 打开编辑抽屉；密码留空表示保持原密码。 */
function openEdit(user: PlatformUserItem) {
  editingUser.value = user;
  Object.assign(form, {
    displayName: user.displayName,
    isPlatformAdmin: user.isPlatformAdmin,
    password: '',
    status: user.status as UserStatus,
    username: user.username,
  });
  drawerVisible.value = true;
}

/** 校验并保存用户。新增时必须设置初始密码，编辑时密码为可选重置项。 */
async function submit() {
  if (!form.username.trim() || !form.displayName.trim()) {
    ElMessage.warning('请填写用户名和姓名');
    return;
  }
  if (!editingUser.value && form.password.length < 8) {
    ElMessage.warning('初始密码至少 8 位');
    return;
  }
  if (editingUser.value && form.password && form.password.length < 8) {
    ElMessage.warning('新密码至少 8 位');
    return;
  }

  submitting.value = true;
  try {
    if (editingUser.value) {
      await updatePlatformUserApi(editingUser.value.id, {
        displayName: form.displayName.trim(),
        ...(form.password ? { password: form.password } : {}),
        status: form.status,
      });
      ElMessage.success('用户资料已更新');
    } else {
      await createPlatformUserApi({
        displayName: form.displayName.trim(),
        isPlatformAdmin: form.isPlatformAdmin,
        password: form.password,
        username: form.username.trim(),
      });
      ElMessage.success('后台用户已创建');
    }
    drawerVisible.value = false;
    await load();
  } finally {
    submitting.value = false;
  }
}

/** 二次确认后授予或撤销安装级超级管理员角色。 */
async function togglePlatformAdmin(user: PlatformUserItem) {
  const enabled = !user.isPlatformAdmin;
  await ElMessageBox.confirm(
    `确定${enabled ? '授予' : '撤销'}「${user.displayName}」的超级管理员权限吗？`,
    enabled ? '授予超级管理员' : '撤销超级管理员',
    {
      confirmButtonText: enabled ? '确认授予' : '确认撤销',
      type: 'warning',
    },
  );
  await setPlatformAdminApi(user.id, enabled);
  ElMessage.success('平台角色已更新');
  await load();
}

/** 将 ISO 时间转为本地可读时间。 */
function formatTime(value: string) {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString('zh-CN');
}

onMounted(load);
</script>

<template>
  <Page
    class="saas-page"
    content-class="saas-page-content"
    description="管理可登录平台后台的账号；租户成员与角色请在空间管理中维护"
    header-class="saas-page-header"
    title="用户管理"
  >
    <template #extra>
      <ElButton
        v-access:code="'user:manage'"
        type="primary"
        @click="openCreate"
      >
        新增用户
      </ElButton>
    </template>

    <div class="saas-kpi-grid">
      <div class="saas-kpi">
        <div class="saas-kpi__label">用户总数</div>
        <div class="saas-kpi__value">{{ users.length }}</div>
        <div class="saas-kpi__hint">安装级后台账号</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">正常账号</div>
        <div class="saas-kpi__value">{{ activeCount }}</div>
        <div class="saas-kpi__hint">当前可以登录</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">超级管理员</div>
        <div class="saas-kpi__value">{{ adminCount }}</div>
        <div class="saas-kpi__hint">拥有全部平台权限</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">已停用</div>
        <div class="saas-kpi__value">{{ disabledCount }}</div>
        <div class="saas-kpi__hint">不可登录但保留历史数据</div>
      </div>
    </div>

    <ElCard>
      <div class="mb-5 flex flex-wrap items-center gap-3">
        <ElInput
          v-model="keyword"
          clearable
          placeholder="搜索用户名或姓名"
          style="width: 260px"
        />
        <ElSelect
          v-model="statusFilter"
          clearable
          placeholder="账号状态"
          style="width: 140px"
        >
          <ElOption label="正常" value="active" />
          <ElOption label="已停用" value="disabled" />
        </ElSelect>
        <ElSelect
          v-model="roleFilter"
          clearable
          placeholder="平台角色"
          style="width: 160px"
        >
          <ElOption label="超级管理员" value="admin" />
          <ElOption label="普通用户" value="user" />
        </ElSelect>
        <ElButton
          v-if="keyword || statusFilter || roleFilter"
          @click="
            keyword = '';
            statusFilter = '';
            roleFilter = '';
          "
        >
          重置
        </ElButton>
        <div class="ml-auto text-sm text-gray-400">
          共 {{ filteredUsers.length }} 条
        </div>
      </div>

      <ElTable v-loading="loading" :data="pagedUsers" row-key="id">
        <ElTableColumn label="用户" min-width="210">
          <template #default="{ row }">
            <div class="font-medium">{{ row.displayName }}</div>
            <div class="mt-1 text-xs text-gray-400">{{ row.username }}</div>
          </template>
        </ElTableColumn>
        <ElTableColumn label="平台角色" width="150">
          <template #default="{ row }">
            <ElTag
              :type="row.isPlatformAdmin ? 'primary' : 'info'"
              effect="plain"
            >
              {{ row.isPlatformAdmin ? '超级管理员' : '普通用户' }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="110">
          <template #default="{ row }">
            <ElTag :type="row.status === 'active' ? 'success' : 'danger'">
              {{ row.status === 'active' ? '正常' : '已停用' }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="创建时间" min-width="180">
          <template #default="{ row }">{{
            formatTime(row.createdAt)
          }}</template>
        </ElTableColumn>
        <ElTableColumn fixed="right" label="操作" width="220">
          <template #default="{ row }">
            <ElButton link type="primary" @click="openEdit(row)">编辑</ElButton>
            <ElButton
              v-access:code="'user:platform-admin'"
              :disabled="row.status !== 'active'"
              link
              :type="row.isPlatformAdmin ? 'danger' : 'primary'"
              @click="togglePlatformAdmin(row)"
            >
              {{ row.isPlatformAdmin ? '撤销管理员' : '设为管理员' }}
            </ElButton>
          </template>
        </ElTableColumn>
      </ElTable>

      <div class="mt-5 flex justify-end">
        <ElPagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50]"
          :total="filteredUsers.length"
          background
          layout="total, sizes, prev, pager, next"
        />
      </div>
    </ElCard>

    <ElDrawer
      v-model="drawerVisible"
      :title="editingUser ? '编辑用户' : '新增用户'"
      destroy-on-close
      size="520px"
    >
      <ElForm label-position="top">
        <ElFormItem label="用户名" required>
          <ElInput
            v-model="form.username"
            :disabled="!!editingUser"
            maxlength="64"
            placeholder="用于登录，创建后不可修改"
          />
        </ElFormItem>
        <ElFormItem label="姓名" required>
          <ElInput
            v-model="form.displayName"
            maxlength="64"
            placeholder="请输入用户姓名"
          />
        </ElFormItem>
        <ElFormItem
          :label="editingUser ? '重置密码' : '初始密码'"
          :required="!editingUser"
        >
          <ElInput
            v-model="form.password"
            maxlength="128"
            :placeholder="editingUser ? '留空表示不修改密码' : '至少 8 位'"
            show-password
            type="password"
          />
        </ElFormItem>
        <ElFormItem v-if="editingUser" label="账号状态" required>
          <ElSelect v-model="form.status" class="w-full">
            <ElOption label="正常" value="active" />
            <ElOption label="停用" value="disabled" />
          </ElSelect>
          <div class="mt-2 text-xs text-gray-400">
            停用后该账号不能登录，但历史数据仍会保留。
          </div>
        </ElFormItem>
        <ElFormItem v-else label="平台角色">
          <div class="flex items-center gap-3">
            <ElSwitch v-model="form.isPlatformAdmin" />
            <span class="text-sm text-gray-500">创建时授予超级管理员</span>
          </div>
        </ElFormItem>
      </ElForm>
      <template #footer>
        <div class="flex justify-end gap-2">
          <ElButton @click="drawerVisible = false">取消</ElButton>
          <ElButton :loading="submitting" type="primary" @click="submit">
            {{ editingUser ? '保存修改' : '创建用户' }}
          </ElButton>
        </div>
      </template>
    </ElDrawer>
  </Page>
</template>
