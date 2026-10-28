<script lang="ts" setup>
import type { KnowledgeBaseItem } from '#/api';

import { onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElButton,
  ElCard,
  ElDialog,
  ElEmpty,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElOption,
  ElSelect,
  ElTag,
  ElUpload,
} from 'element-plus';

import {
  createKnowledgeBaseApi,
  listKnowledgeBasesApi,
  uploadKnowledgeDocumentApi,
} from '#/api';

const loading = ref(false);
const dialogVisible = ref(false);
const items = ref<KnowledgeBaseItem[]>([]);
const form = reactive<{ name: string; visibility: 'private' | 'tenant' }>({
  name: '',
  visibility: 'private',
});

/** 加载知识库列表，并同步加载每个知识库的文档。 */
async function load() {
  loading.value = true;
  try {
    items.value = await listKnowledgeBasesApi();
  } finally {
    loading.value = false;
  }
}

/** 创建知识库后关闭弹窗并刷新列表。 */
async function create() {
  if (!form.name.trim()) return;
  await createKnowledgeBaseApi({
    name: form.name.trim(),
    visibility: form.visibility,
  });
  dialogVisible.value = false;
  form.name = '';
  await load();
}

/** 上传单个知识库文档并刷新索引状态列表。 */
async function upload(knowledgeBaseID: string, file: File) {
  await uploadKnowledgeDocumentApi(knowledgeBaseID, file);
  ElMessage.success('文件已上传，等待 Dify 建立索引');
}

onMounted(load);
</script>

<template>
  <Page
    description="个人资料默认私有；多租户模式可显式共享到当前租户"
    title="私有知识库"
  >
    <template #extra>
      <ElButton type="primary" @click="dialogVisible = true"
        >新建知识库</ElButton
      >
    </template>
    <div v-loading="loading" class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      <ElCard v-for="item in items" :key="item.id" shadow="hover">
        <template #header>
          <div class="flex items-center justify-between">
            <strong>{{ item.name }}</strong>
            <ElTag>{{
              item.visibility === 'private' ? '私有' : '租户共享'
            }}</ElTag>
          </div>
        </template>
        <div class="text-sm text-gray-500">状态：{{ item.status }}</div>
        <ElUpload
          class="mt-4"
          :http-request="(options) => upload(item.id, options.file)"
          :show-file-list="false"
        >
          <ElButton>上传文档</ElButton>
        </ElUpload>
      </ElCard>
    </div>
    <ElEmpty v-if="!loading && items.length === 0" description="还没有知识库" />

    <ElDialog v-model="dialogVisible" title="新建知识库" width="480px">
      <ElForm label-width="90px">
        <ElFormItem label="名称">
          <ElInput v-model="form.name" maxlength="100" />
        </ElFormItem>
        <ElFormItem label="可见范围">
          <ElSelect v-model="form.visibility" class="w-full">
            <ElOption label="仅自己" value="private" />
            <ElOption label="当前租户" value="tenant" />
          </ElSelect>
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="dialogVisible = false">取消</ElButton>
        <ElButton type="primary" @click="create">创建</ElButton>
      </template>
    </ElDialog>
  </Page>
</template>
