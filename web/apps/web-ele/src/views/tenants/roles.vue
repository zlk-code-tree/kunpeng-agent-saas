<script lang="ts" setup>
import type {
  PermissionItem,
  RoleItem,
  TenantItem,
  TenantMemberItem,
} from '#/api';

import { computed, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElButton,
  ElCard,
  ElCheckbox,
  ElCheckboxGroup,
  ElDrawer,
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
  createTenantRoleApi,
  deleteTenantRoleApi,
  getMemberRolesApi,
  listPermissionsApi,
  listTenantMembersApi,
  listTenantRolesApi,
  listTenantsApi,
  setMemberRolesApi,
  updateTenantMemberRoleApi,
  updateTenantRoleApi,
} from '#/api';

type BuiltinRole = 'admin' | 'finance' | 'member';

const loading = ref(false);
const saving = ref(false);
const roles = ref<RoleItem[]>([]);
const permissions = ref<PermissionItem[]>([]);
const members = ref<TenantMemberItem[]>([]);
const tenants = ref<TenantItem[]>([]);
const roleKeyword = ref('');
const memberKeyword = ref('');
const roleTypeFilter = ref('');
const roleDrawerVisible = ref(false);
const editingRole = ref<RoleItem>();
const memberDrawerVisible = ref(false);
const assigningMember = ref<TenantMemberItem>();
const assignRoleIds = ref<string[]>([]);
const assignBuiltinRole = ref<BuiltinRole>('member');

const roleForm = reactive({
  description: '',
  name: '',
  permissionCodes: [] as string[],
});

const currentTenant = computed(() =>
  tenants.value.find((item) => item.current),
);
const customRoles = computed(() => roles.value.filter((item) => !item.builtin));
const canChangeBuiltinRole = computed(
  () => currentTenant.value?.role === 'owner',
);

const filteredRoles = computed(() => {
  const search = roleKeyword.value.trim().toLowerCase();
  return roles.value.filter((item) => {
    const matchesKeyword =
      !search ||
      item.name.toLowerCase().includes(search) ||
      item.code.toLowerCase().includes(search) ||
      item.description.toLowerCase().includes(search);
    const matchesType =
      !roleTypeFilter.value ||
      (roleTypeFilter.value === 'builtin' ? item.builtin : !item.builtin);
    return matchesKeyword && matchesType;
  });
});

const filteredMembers = computed(() => {
  const search = memberKeyword.value.trim().toLowerCase();
  if (!search) return members.value;
  return members.value.filter(
    (item) =>
      item.username.toLowerCase().includes(search) ||
      item.displayName.toLowerCase().includes(search),
  );
});

/** 仅使用租户作用域权限，并按业务模块分组渲染角色授权面板。 */
const permissionGroups = computed(() => {
  const byName = new Map<string, PermissionItem[]>();
  for (const item of permissions.value) {
    if (item.scope !== 'tenant') continue;
    const group = byName.get(item.group) ?? [];
    group.push(item);
    byName.set(item.group, group);
  }
  return [...byName.entries()].map(([name, items]) => ({ items, name }));
});

const permissionNameMap = computed(
  () => new Map(permissions.value.map((item) => [item.code, item.name])),
);

/** 加载当前空间的角色、权限目录和成员。 */
async function load() {
  loading.value = true;
  try {
    [roles.value, permissions.value, tenants.value] = await Promise.all([
      listTenantRolesApi(),
      listPermissionsApi(),
      listTenantsApi(),
    ]);
    members.value = currentTenant.value
      ? await listTenantMembersApi(currentTenant.value.id)
      : [];
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  editingRole.value = undefined;
  roleForm.name = '';
  roleForm.description = '';
  roleForm.permissionCodes = [];
  roleDrawerVisible.value = true;
}

function openEdit(role: RoleItem) {
  if (role.builtin) return;
  editingRole.value = role;
  roleForm.name = role.name;
  roleForm.description = role.description;
  roleForm.permissionCodes = [...role.permissionCodes];
  roleDrawerVisible.value = true;
}

/** 对一个业务分组执行全选或取消全选，同时保留其他分组的选择。 */
function toggleGroup(items: PermissionItem[], checked: boolean) {
  const codes = new Set(roleForm.permissionCodes);
  for (const item of items) {
    if (checked) codes.add(item.code);
    else codes.delete(item.code);
  }
  roleForm.permissionCodes = [...codes];
}

function isGroupChecked(items: PermissionItem[]) {
  return items.every((item) => roleForm.permissionCodes.includes(item.code));
}

async function saveRole() {
  if (!roleForm.name.trim()) {
    ElMessage.warning('请填写角色名称');
    return;
  }
  if (roleForm.permissionCodes.length === 0) {
    ElMessage.warning('请至少选择一个权限');
    return;
  }
  saving.value = true;
  try {
    const payload = {
      description: roleForm.description.trim(),
      name: roleForm.name.trim(),
      permissionCodes: roleForm.permissionCodes,
    };
    if (editingRole.value) {
      await updateTenantRoleApi(editingRole.value.id, payload);
      ElMessage.success('角色已更新');
    } else {
      await createTenantRoleApi(payload);
      ElMessage.success('角色已创建');
    }
    roleDrawerVisible.value = false;
    await load();
  } finally {
    saving.value = false;
  }
}

async function removeRole(role: RoleItem) {
  await ElMessageBox.confirm(
    `删除「${role.name}」后，成员与该角色的绑定也会清除，是否继续？`,
    '删除自定义角色',
    { confirmButtonText: '确认删除', type: 'warning' },
  );
  await deleteTenantRoleApi(role.id);
  ElMessage.success('角色已删除');
  await load();
}

/** 在同一个抽屉中展示成员主角色与附加自定义角色，减少跨页面授权。 */
async function openAssign(member: TenantMemberItem) {
  if (member.role === 'owner') return;
  assigningMember.value = member;
  assignBuiltinRole.value = member.role as BuiltinRole;
  const result = await getMemberRolesApi(member.userId);
  assignRoleIds.value = result.roleIds;
  memberDrawerVisible.value = true;
}

/** 所有者可调整主角色；具有分配权限的管理员可维护附加自定义角色。 */
async function saveAssign() {
  const tenant = currentTenant.value;
  const member = assigningMember.value;
  if (!tenant || !member) return;
  saving.value = true;
  try {
    if (canChangeBuiltinRole.value && assignBuiltinRole.value !== member.role) {
      await updateTenantMemberRoleApi(
        tenant.id,
        member.userId,
        assignBuiltinRole.value,
      );
    }
    await setMemberRolesApi(member.userId, assignRoleIds.value);
    ElMessage.success('成员角色已更新');
    memberDrawerVisible.value = false;
    await load();
  } finally {
    saving.value = false;
  }
}

function roleLabel(role: string) {
  return (
    { admin: '管理员', finance: '财务', member: '成员', owner: '所有者' }[
      role
    ] ?? role
  );
}

onMounted(load);
</script>

<template>
  <Page
    class="saas-page"
    content-class="saas-page-content"
    :description="`角色和成员授权仅作用于当前空间：${currentTenant?.name || '未选择空间'}`"
    header-class="saas-page-header"
    title="角色管理"
  >
    <template #extra>
      <ElButton
        v-access:code="'role:manage'"
        type="primary"
        @click="openCreate"
      >
        新建角色
      </ElButton>
    </template>

    <div class="saas-kpi-grid">
      <div class="saas-kpi">
        <div class="saas-kpi__label">角色总数</div>
        <div class="saas-kpi__value">{{ roles.length }}</div>
        <div class="saas-kpi__hint">当前空间可用角色</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">内置角色</div>
        <div class="saas-kpi__value">
          {{ roles.length - customRoles.length }}
        </div>
        <div class="saas-kpi__hint">随系统版本维护</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">自定义角色</div>
        <div class="saas-kpi__value">{{ customRoles.length }}</div>
        <div class="saas-kpi__hint">仅当前空间使用</div>
      </div>
      <div class="saas-kpi">
        <div class="saas-kpi__label">空间成员</div>
        <div class="saas-kpi__value">{{ members.length }}</div>
        <div class="saas-kpi__hint">可分配主角色与附加角色</div>
      </div>
    </div>

    <div v-loading="loading" class="grid gap-5">
      <ElCard>
        <template #header>
          <div class="saas-section-title">角色列表</div>
        </template>
        <div class="mb-5 flex flex-wrap items-center gap-3">
          <ElInput
            v-model="roleKeyword"
            clearable
            placeholder="搜索角色名称、标识或说明"
            style="width: 290px"
          />
          <ElSelect
            v-model="roleTypeFilter"
            clearable
            placeholder="角色类型"
            style="width: 150px"
          >
            <ElOption label="内置角色" value="builtin" />
            <ElOption label="自定义角色" value="custom" />
          </ElSelect>
          <div class="ml-auto text-sm text-gray-400">
            共 {{ filteredRoles.length }} 个角色
          </div>
        </div>
        <ElTable :data="filteredRoles" row-key="id">
          <ElTableColumn label="角色" min-width="180">
            <template #default="{ row }">
              <div class="font-medium">{{ row.name }}</div>
              <code class="mt-1 inline-block text-xs text-gray-400">{{
                row.code
              }}</code>
            </template>
          </ElTableColumn>
          <ElTableColumn label="类型" width="110">
            <template #default="{ row }">
              <ElTag :type="row.builtin ? 'info' : 'success'" effect="plain">
                {{ row.builtin ? '内置' : '自定义' }}
              </ElTag>
            </template>
          </ElTableColumn>
          <ElTableColumn
            label="说明"
            min-width="220"
            prop="description"
            show-overflow-tooltip
          />
          <ElTableColumn label="权限" min-width="310">
            <template #default="{ row }">
              <div class="flex flex-wrap gap-1">
                <ElTag
                  v-for="code in row.permissionCodes.slice(0, 4)"
                  :key="code"
                  effect="plain"
                  size="small"
                >
                  {{ permissionNameMap.get(code) || code }}
                </ElTag>
                <ElTag
                  v-if="row.permissionCodes.length > 4"
                  effect="plain"
                  size="small"
                  type="info"
                >
                  +{{ row.permissionCodes.length - 4 }}
                </ElTag>
              </div>
            </template>
          </ElTableColumn>
          <ElTableColumn label="权限数" width="90">
            <template #default="{ row }">{{
              row.permissionCodes.length
            }}</template>
          </ElTableColumn>
          <ElTableColumn fixed="right" label="操作" width="130">
            <template #default="{ row }">
              <template v-if="!row.builtin">
                <ElButton link type="primary" @click="openEdit(row)"
                  >编辑</ElButton
                >
                <ElButton link type="danger" @click="removeRole(row)"
                  >删除</ElButton
                >
              </template>
              <span v-else class="text-xs text-gray-400">系统维护</span>
            </template>
          </ElTableColumn>
        </ElTable>
      </ElCard>

      <ElCard>
        <template #header>
          <div class="saas-section-title">成员角色分配</div>
        </template>
        <div class="mb-5 flex items-center gap-3">
          <ElInput
            v-model="memberKeyword"
            clearable
            placeholder="搜索成员姓名或用户名"
            style="width: 290px"
          />
          <div class="ml-auto text-sm text-gray-400">
            当前空间 · {{ currentTenant?.name }}
          </div>
        </div>
        <ElTable :data="filteredMembers" row-key="userId">
          <ElTableColumn label="成员" min-width="240">
            <template #default="{ row }">
              <div class="font-medium">{{ row.displayName }}</div>
              <div class="mt-1 text-xs text-gray-400">{{ row.username }}</div>
            </template>
          </ElTableColumn>
          <ElTableColumn label="主角色" width="140">
            <template #default="{ row }">
              <ElTag
                :type="row.role === 'owner' ? 'warning' : 'info'"
                effect="plain"
              >
                {{ roleLabel(row.role) }}
              </ElTag>
            </template>
          </ElTableColumn>
          <ElTableColumn label="状态" width="110">
            <template #default="{ row }">
              <ElTag type="success">{{
                row.status === 'active' ? '正常' : row.status
              }}</ElTag>
            </template>
          </ElTableColumn>
          <ElTableColumn label="授权说明" min-width="260">
            <template #default="{ row }">
              {{
                row.role === 'owner'
                  ? '所有者拥有当前空间全部权限'
                  : '可叠加一个或多个自定义角色'
              }}
            </template>
          </ElTableColumn>
          <ElTableColumn fixed="right" label="操作" width="130">
            <template #default="{ row }">
              <ElButton
                :disabled="row.role === 'owner'"
                link
                type="primary"
                @click="openAssign(row)"
              >
                分配角色
              </ElButton>
            </template>
          </ElTableColumn>
        </ElTable>
      </ElCard>
    </div>

    <ElDrawer
      v-model="roleDrawerVisible"
      :title="editingRole ? '编辑自定义角色' : '新建自定义角色'"
      destroy-on-close
      size="680px"
    >
      <ElForm label-position="top">
        <ElFormItem label="角色名称" required>
          <ElInput
            v-model="roleForm.name"
            maxlength="80"
            placeholder="例如：知识库运营"
          />
        </ElFormItem>
        <ElFormItem label="角色说明">
          <ElInput
            v-model="roleForm.description"
            :rows="3"
            maxlength="255"
            placeholder="说明该角色适用的人员和职责"
            show-word-limit
            type="textarea"
          />
        </ElFormItem>
        <ElFormItem label="权限配置" required>
          <div class="grid w-full gap-3">
            <div
              v-for="group in permissionGroups"
              :key="group.name"
              class="rounded-lg border border-gray-200 p-4 dark:border-gray-700"
            >
              <div class="mb-3 flex items-center justify-between">
                <span class="font-medium">{{ group.name }}</span>
                <ElButton
                  link
                  type="primary"
                  @click="
                    toggleGroup(group.items, !isGroupChecked(group.items))
                  "
                >
                  {{ isGroupChecked(group.items) ? '取消全选' : '全选本组' }}
                </ElButton>
              </div>
              <ElCheckboxGroup
                v-model="roleForm.permissionCodes"
                class="grid grid-cols-2 gap-x-4"
              >
                <ElCheckbox
                  v-for="item in group.items"
                  :key="item.code"
                  :value="item.code"
                  class="!mr-0 !h-auto py-2"
                >
                  <div>
                    <div>{{ item.name }}</div>
                    <div class="text-xs text-gray-400">
                      {{ item.description }}
                    </div>
                  </div>
                </ElCheckbox>
              </ElCheckboxGroup>
            </div>
          </div>
        </ElFormItem>
      </ElForm>
      <template #footer>
        <div class="flex justify-end gap-2">
          <ElButton @click="roleDrawerVisible = false">取消</ElButton>
          <ElButton :loading="saving" type="primary" @click="saveRole"
            >保存角色</ElButton
          >
        </div>
      </template>
    </ElDrawer>

    <ElDrawer
      v-model="memberDrawerVisible"
      :title="`分配角色 · ${assigningMember?.displayName || ''}`"
      destroy-on-close
      size="520px"
    >
      <ElForm label-position="top">
        <ElFormItem label="成员账号">
          <ElInput :model-value="assigningMember?.username" disabled />
        </ElFormItem>
        <ElFormItem label="主角色">
          <ElSelect
            v-model="assignBuiltinRole"
            :disabled="!canChangeBuiltinRole"
            class="w-full"
          >
            <ElOption label="管理员" value="admin" />
            <ElOption label="财务" value="finance" />
            <ElOption label="成员" value="member" />
          </ElSelect>
          <div class="mt-2 text-xs text-gray-400">
            {{
              canChangeBuiltinRole
                ? '只有空间所有者可以修改成员主角色。'
                : '当前账号不是空间所有者，不能修改主角色。'
            }}
          </div>
        </ElFormItem>
        <ElFormItem label="附加自定义角色">
          <ElSelect
            v-model="assignRoleIds"
            class="w-full"
            collapse-tags
            collapse-tags-tooltip
            multiple
            placeholder="可多选；留空表示只使用主角色"
          >
            <ElOption
              v-for="role in customRoles"
              :key="role.id"
              :label="role.name"
              :value="role.id"
            />
          </ElSelect>
          <div
            v-if="customRoles.length === 0"
            class="mt-2 text-xs text-gray-400"
          >
            当前空间还没有自定义角色，请先创建角色。
          </div>
        </ElFormItem>
      </ElForm>
      <template #footer>
        <div class="flex justify-end gap-2">
          <ElButton @click="memberDrawerVisible = false">取消</ElButton>
          <ElButton :loading="saving" type="primary" @click="saveAssign"
            >保存授权</ElButton
          >
        </div>
      </template>
    </ElDrawer>
  </Page>
</template>
