<script lang="ts" setup>
import { onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';

import {
  ElButton,
  ElCard,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElTag,
} from 'element-plus';

import { getInstallStatusApi, installPlatformApi } from '#/api';

const router = useRouter();
const loading = ref(false);
const mode = ref<'multi' | 'single'>('single');
const form = reactive({
  adminName: '平台管理员',
  adminPassword: '',
  adminUsername: 'admin',
  platformName: '智能体 SaaS 平台',
  tenantName: '默认空间',
});

/** 提交首次安装表单，成功后进入登录页。 */
async function install() {
  loading.value = true;
  try {
    await installPlatformApi(form);
    ElMessage.success('初始化完成，请登录');
    await router.replace('/auth/login');
  } finally {
    loading.value = false;
  }
}

onMounted(async () => {
  const status = await getInstallStatusApi();
  mode.value = status.mode;
  if (status.installed) await router.replace('/auth/login');
});
</script>

<template>
  <ElCard class="w-full max-w-xl" shadow="never">
    <template #header>
      <div class="flex items-center justify-between">
        <strong class="text-lg">初始化智能体平台</strong>
        <ElTag>{{ mode === 'multi' ? '多租户模式' : '单实例模式' }}</ElTag>
      </div>
    </template>
    <ElForm :model="form" label-position="top">
      <ElFormItem label="平台名称"
        ><ElInput v-model="form.platformName"
      /></ElFormItem>
      <ElFormItem label="默认空间"
        ><ElInput v-model="form.tenantName"
      /></ElFormItem>
      <div class="grid grid-cols-2 gap-4">
        <ElFormItem label="管理员账号"
          ><ElInput v-model="form.adminUsername"
        /></ElFormItem>
        <ElFormItem label="管理员名称"
          ><ElInput v-model="form.adminName"
        /></ElFormItem>
      </div>
      <ElFormItem label="管理员密码">
        <ElInput
          v-model="form.adminPassword"
          minlength="8"
          show-password
          type="password"
        />
      </ElFormItem>
      <ElButton
        class="w-full"
        :loading="loading"
        type="primary"
        @click="install"
        >完成初始化</ElButton
      >
    </ElForm>
  </ElCard>
</template>
