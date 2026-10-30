<script lang="ts" setup>
import type { PlatformUserItem, TenantItem, TenantMemberItem } from '#/api';

import { computed, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElButton,
  ElCard,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElSelect,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import {
  listPlatformUsersApi,
  listTenantMembersApi,
  listTenantsApi,
  setPlatformAdminApi,
  updateTenantMemberRoleApi,
} from '#/api';

type EditableRole = 'admin' | 'finance' | 'member';

const users = ref<PlatformUserItem[]>([]);
const tenants = ref<TenantItem[]>([]);
const members = ref<TenantMemberItem[]>([]);
const memberRoles = reactive<Record<string, EditableRole>>({});
const loading = ref(false);
const saving = ref('');
const currentTenant = computed(() =>
  tenants.value.find((item) => item.current),
);
const canChangeMemberRole = computed(
  () => currentTenant.value?.role === 'owner',
);
const platformAdminCount = computed(
  () => users.value.filter((item) => item.isPlatformAdmin).length,
);

// 当前角色规则由后端固定实现；这里明确展示授权边界，不提供虚假的细粒度开关。
const roleRules = [
  {
    role: '平台管理员',
    scope: '安装级',
    permissions: '平台设置、企业集成、菜单管理、账号授权',
  },
  {
    role: '租户所有者',
    scope: '当前租户',
    permissions: '成员邀请、角色调整、移除、所有权转移',
  },
  {
    role: '租户管理员',
    scope: '当前租户',
    permissions: '成员邀请与普通成员移除',
  },
  {
    role: '财务',
    scope: '当前租户',
    permissions: '当前版本与成员共享常规功能及账本查看',
  },
  {
    role: '成员',
    scope: '当前租户',
    permissions: '智能体、私有知识库与当前租户账本查看',
  },
];

/** 加载平台用户和当前空间成员；平台权限与租户权限分别展示。 */
async function load() {
  loading.value = true;
  try {
    [users.value, tenants.value] = await Promise.all([
      listPlatformUsersApi(),
      listTenantsApi(),
    ]);
    members.value = currentTenant.value
      ? await listTenantMembersApi(currentTenant.value.id)
      : [];
    for (const member of members.value) {
      if (member.role !== 'owner') {
        memberRoles[member.userId] = member.role;
      }
    }
  } finally {
    loading.value = false;
  }
}

/** 变更安装级管理员，二次确认并由服务端保护最后一位管理员。 */
async function togglePlatformAdmin(user: PlatformUserItem) {
  const enabled = !user.isPlatformAdmin;
  await ElMessageBox.confirm(
    `确定${enabled ? '授予' : '撤销'} ${user.username} 的平台管理员权限吗？`,
    '平台权限变更',
    { type: 'warning' },
  );
  saving.value = user.id;
  try {
    await setPlatformAdminApi(user.id, enabled);
    ElMessage.success('平台权限已更新');
    users.value = await listPlatformUsersApi();
  } finally {
    saving.value = '';
  }
}

/** 租户所有者保存成员角色；owner 只能走所有权转移接口。 */
async function saveMemberRole(member: TenantMemberItem) {
  const tenant = currentTenant.value;
  const role = memberRoles[member.userId];
  if (!tenant || !role || role === member.role) return;
  saving.value = member.userId;
  try {
    await updateTenantMemberRoleApi(tenant.id, member.userId, role);
    ElMessage.success('成员角色已更新');
    members.value = await listTenantMembersApi(tenant.id);
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
    description="安装级平台管理员与租户角色分别授权；角色修改在下一次请求立即生效"
    header-class="saas-page-header"
    title="权限管理"
  >
    <div v-loading="loading" class="grid gap-5">
      <div class="saas-kpi-grid !mb-0">
        <div class="saas-kpi">
          <div class="saas-kpi__label">平台管理员</div>
          <div class="saas-kpi__value">{{ platformAdminCount }}</div>
          <div class="saas-kpi__hint">安装级管理权限</div>
        </div>
        <div class="saas-kpi">
          <div class="saas-kpi__label">当前空间成员</div>
          <div class="saas-kpi__value">{{ members.length }}</div>
          <div class="saas-kpi__hint">
            {{ currentTenant?.name || '未选择空间' }}
          </div>
        </div>
        <div class="saas-kpi">
          <div class="saas-kpi__label">角色类型</div>
          <div class="saas-kpi__value">{{ roleRules.length }}</div>
          <div class="saas-kpi__hint">平台与租户两级授权</div>
        </div>
      </div>

      <ElCard>
        <template #header>
          <div class="saas-section-title">角色权限规则</div>
        </template>
        <ElTable :data="roleRules">
          <ElTableColumn label="角色" prop="role" width="170" />
          <ElTableColumn label="作用范围" prop="scope" width="140" />
          <ElTableColumn label="当前权限" min-width="340" prop="permissions" />
        </ElTable>
      </ElCard>

      <ElCard>
        <template #header>
          <div class="saas-section-title">平台管理员账号</div>
        </template>
        <ElTable :data="users" row-key="id">
          <ElTableColumn label="用户名" prop="username" />
          <ElTableColumn label="姓名" prop="displayName" />
          <ElTableColumn label="状态" prop="status" width="120" />
          <ElTableColumn label="平台角色" width="160">
            <template #default="{ row }">
              <ElTag :type="row.isPlatformAdmin ? 'success' : 'info'">
                {{ row.isPlatformAdmin ? '平台管理员' : '普通用户' }}
              </ElTag>
            </template>
          </ElTableColumn>
          <ElTableColumn label="操作" width="130">
            <template #default="{ row }">
              <ElButton
                :disabled="row.status !== 'active'"
                :loading="saving === row.id"
                link
                type="primary"
                @click="togglePlatformAdmin(row)"
                >{{ row.isPlatformAdmin ? '撤销' : '授予' }}</ElButton
              >
            </template>
          </ElTableColumn>
        </ElTable>
      </ElCard>

      <ElCard>
        <template #header>
          <div class="saas-section-title">
            当前空间成员 · {{ currentTenant?.name || '未选择' }}
          </div>
        </template>
        <ElTable :data="members" row-key="userId">
          <ElTableColumn label="用户名" prop="username" />
          <ElTableColumn label="姓名" prop="displayName" />
          <ElTableColumn label="租户角色" min-width="220">
            <template #default="{ row }">
              <ElTag v-if="row.role === 'owner'">所有者</ElTag>
              <ElSelect
                v-else
                v-model="memberRoles[row.userId]"
                :disabled="!canChangeMemberRole"
                class="w-40"
              >
                <ElOption label="管理员" value="admin" />
                <ElOption label="财务" value="finance" />
                <ElOption label="成员" value="member" />
              </ElSelect>
            </template>
          </ElTableColumn>
          <ElTableColumn label="操作" width="130">
            <template #default="{ row }">
              <ElButton
                v-if="row.role !== 'owner' && canChangeMemberRole"
                :disabled="memberRoles[row.userId] === row.role"
                :loading="saving === row.userId"
                link
                type="primary"
                @click="saveMemberRole(row)"
                >保存</ElButton
              >
            </template>
          </ElTableColumn>
        </ElTable>
      </ElCard>
    </div>
  </Page>
</template>
