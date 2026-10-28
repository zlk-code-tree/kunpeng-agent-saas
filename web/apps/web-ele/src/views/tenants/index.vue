<script lang="ts" setup>
import type { TenantItem, TenantMemberItem } from '#/api';

import { computed, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';
import { useAccessStore, useUserStore } from '@vben/stores';

import {
  ElButton,
  ElCard,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElSelect,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import {
  acceptTenantInvitationApi,
  createTenantApi,
  inviteTenantMemberApi,
  listTenantMembersApi,
  listTenantsApi,
  removeTenantMemberApi,
  switchTenantApi,
  transferTenantOwnershipApi,
} from '#/api';

const accessStore = useAccessStore();
const userStore = useUserStore();
const loading = ref(false);
const tenants = ref<TenantItem[]>([]);
const members = ref<TenantMemberItem[]>([]);
const createVisible = ref(false);
const inviteVisible = ref(false);
const acceptVisible = ref(false);
const createForm = reactive({ name: '' });
const inviteForm = reactive<{
  role: 'admin' | 'finance' | 'member';
  username: string;
}>({ role: 'member', username: '' });
const invitationToken = ref('');
const currentTenant = computed(() =>
  tenants.value.find((item) => item.current),
);
const canManage = computed(() =>
  ['owner', 'admin'].includes(currentTenant.value?.role || ''),
);

/** 加载租户列表，并仅加载当前租户的成员。 */
async function load() {
  loading.value = true;
  try {
    tenants.value = await listTenantsApi();
    members.value = currentTenant.value
      ? await listTenantMembersApi(currentTenant.value.id)
      : [];
  } finally {
    loading.value = false;
  }
}

/** 创建企业空间并刷新列表。 */
async function createTenant() {
  if (!createForm.name.trim()) return;
  await createTenantApi({ name: createForm.name.trim() });
  createVisible.value = false;
  createForm.name = '';
  ElMessage.success('企业空间已创建');
  await load();
}

/** 切换租户令牌后整页跳转，确保所有 Store 和权限路由按新租户重新初始化。 */
async function switchTenant(item: TenantItem) {
  if (item.current || item.status !== 'active') return;
  const result = await switchTenantApi(item.id);
  accessStore.setAccessToken(result.accessToken);
  window.location.assign('/workspace');
}

/** 创建成员邀请，并只在本次弹窗展示一次原始邀请令牌。 */
async function invite() {
  const current = currentTenant.value;
  if (!current || !inviteForm.username.trim()) return;
  const result = await inviteTenantMemberApi(current.id, {
    role: inviteForm.role,
    username: inviteForm.username.trim(),
  });
  inviteVisible.value = false;
  inviteForm.username = '';
  await ElMessageBox.alert(
    `请将以下一次性邀请凭证发送给 ${result.username}：\n\n${result.token}\n\n有效期至 ${new Date(result.expiresAt).toLocaleString()}`,
    '成员邀请已创建',
    { confirmButtonText: '我已保存', type: 'success' },
  );
}

/** 使用邀请令牌加入企业空间并刷新列表。 */
async function acceptInvitation() {
  if (!invitationToken.value.trim()) return;
  await acceptTenantInvitationApi(invitationToken.value.trim());
  invitationToken.value = '';
  acceptVisible.value = false;
  ElMessage.success('已加入企业空间');
  await load();
}

/** 二次确认后移除成员，并刷新当前租户成员列表。 */
async function removeMember(member: TenantMemberItem) {
  const current = currentTenant.value;
  if (!current) return;
  await ElMessageBox.confirm(
    `确定移除成员 ${member.displayName} 吗？`,
    '移除成员',
    {
      type: 'warning',
    },
  );
  await removeTenantMemberApi(current.id, member.userId);
  members.value = await listTenantMembersApi(current.id);
}

/** 二次确认后转移租户所有权，并重新加载当前用户角色。 */
async function transferOwnership(member: TenantMemberItem) {
  const current = currentTenant.value;
  if (!current) return;
  await ElMessageBox.confirm(
    `确定把 ${current.name} 的所有权转移给 ${member.displayName} 吗？`,
    '转移所有权',
    { type: 'warning' },
  );
  await transferTenantOwnershipApi(current.id, member.userId);
  ElMessage.success('所有权已转移，请重新进入该空间刷新权限');
  await load();
}

onMounted(load);
</script>

<template>
  <Page
    description="个人空间与企业空间使用相同的租户隔离模型；切换空间会签发新的租户身份令牌"
    title="空间与成员"
  >
    <template #extra>
      <div class="flex gap-2">
        <ElButton @click="acceptVisible = true">接受邀请</ElButton>
        <ElButton type="primary" @click="createVisible = true"
          >创建企业空间</ElButton
        >
      </div>
    </template>

    <div v-loading="loading" class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      <ElCard v-for="item in tenants" :key="item.id" shadow="hover">
        <template #header>
          <div class="flex items-center justify-between">
            <strong>{{ item.name }}</strong>
            <ElTag :type="item.current ? 'success' : 'info'">
              {{
                item.current
                  ? '当前空间'
                  : item.type === 'personal'
                    ? '个人'
                    : '企业'
              }}
            </ElTag>
          </div>
        </template>
        <p class="mb-4 text-sm text-gray-500">
          角色：{{ item.role }} · 状态：{{ item.status }}
        </p>
        <ElButton
          :disabled="item.current || item.status !== 'active'"
          @click="switchTenant(item)"
        >
          {{ item.current ? '正在使用' : '切换到此空间' }}
        </ElButton>
      </ElCard>
    </div>

    <ElCard
      v-if="currentTenant"
      class="mt-5"
      :header="`${currentTenant.name} 成员`"
    >
      <template #header>
        <div class="flex items-center justify-between">
          <strong>{{ currentTenant.name }} 成员</strong>
          <ElButton
            v-if="canManage"
            type="primary"
            @click="inviteVisible = true"
            >邀请成员</ElButton
          >
        </div>
      </template>
      <ElTable :data="members">
        <ElTableColumn label="成员" min-width="180">
          <template #default="{ row }"
            >{{ row.displayName }}（{{ row.username }}）</template
          >
        </ElTableColumn>
        <ElTableColumn label="角色" prop="role" width="120" />
        <ElTableColumn label="加入时间" width="190">
          <template #default="{ row }">{{
            new Date(row.joinedAt).toLocaleString()
          }}</template>
        </ElTableColumn>
        <ElTableColumn v-if="canManage" label="操作" width="220">
          <template #default="{ row }">
            <ElButton
              v-if="currentTenant?.role === 'owner' && row.role !== 'owner'"
              link
              type="primary"
              @click="transferOwnership(row)"
              >转移所有权</ElButton
            >
            <ElButton
              v-if="
                row.role !== 'owner' &&
                row.userId !== userStore.userInfo?.userId
              "
              link
              type="danger"
              @click="removeMember(row)"
              >移除</ElButton
            >
          </template>
        </ElTableColumn>
      </ElTable>
    </ElCard>

    <ElDialog v-model="createVisible" title="创建企业空间" width="480px">
      <ElForm label-width="90px">
        <ElFormItem label="空间名称"
          ><ElInput v-model="createForm.name" maxlength="100"
        /></ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="createVisible = false">取消</ElButton>
        <ElButton type="primary" @click="createTenant">创建</ElButton>
      </template>
    </ElDialog>

    <ElDialog v-model="inviteVisible" title="邀请已有用户" width="480px">
      <ElForm label-width="90px">
        <ElFormItem label="用户名"
          ><ElInput v-model="inviteForm.username"
        /></ElFormItem>
        <ElFormItem label="角色">
          <ElSelect v-model="inviteForm.role" class="w-full">
            <ElOption label="管理员" value="admin" />
            <ElOption label="普通成员" value="member" />
            <ElOption label="财务" value="finance" />
          </ElSelect>
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="inviteVisible = false">取消</ElButton>
        <ElButton type="primary" @click="invite">生成邀请凭证</ElButton>
      </template>
    </ElDialog>

    <ElDialog v-model="acceptVisible" title="接受空间邀请" width="520px">
      <ElInput
        v-model="invitationToken"
        :rows="3"
        placeholder="粘贴管理员发送的一次性邀请凭证"
        type="textarea"
      />
      <template #footer>
        <ElButton @click="acceptVisible = false">取消</ElButton>
        <ElButton type="primary" @click="acceptInvitation">加入空间</ElButton>
      </template>
    </ElDialog>
  </Page>
</template>
