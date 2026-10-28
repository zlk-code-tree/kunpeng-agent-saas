<script lang="ts" setup>
import type { AgentItem, RunItem } from '#/api';

import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';

import { Page } from '@vben/common-ui';

import { ElButton, ElCard, ElInput, ElOption, ElSelect, ElTag } from 'element-plus';

import {
  cancelRunApi,
  createRunApi,
  getRunApi,
  listAgentsApi,
  streamRunEventsApi,
} from '#/api';

const route = useRoute();
const prompt = ref('');
const sending = ref(false);
const agents = ref<AgentItem[]>([]);
const selectedAgentID = ref('');
const conversationID = ref('');
const currentRunID = ref('');
const messages = ref<Array<{ content: string; role: 'assistant' | 'user' }>>([
  { content: '请选择已绑定 Dify 应用的智能体，然后输入你的问题。', role: 'assistant' },
]);

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForRun(run: RunItem) {
  let current = run;
  for (let index = 0; index < 120; index++) {
    if (['failed', 'stopped', 'succeeded'].includes(current.status)) return current;
    await sleep(1000);
    current = await getRunApi(run.id);
  }
  throw new Error('运行等待超时，可稍后在运行记录中查看');
}

function outputText(output: RunItem['output']) {
  if (output?.answer) return output.answer;
  if (output?.outputs) return JSON.stringify(output.outputs, null, 2);
  return '';
}

async function streamRun(run: RunItem, messageIndex: number) {
  let buffer = '';
  let lastEventID = 0;
  let terminal = false;
  let receivedDelta = false;

  const handleFrame = (frame: string) => {
    if (!frame.trim()) return;
    const lines = frame.split('\n');
    const idLine = lines.find((line) => line.startsWith('id:'));
    const eventLine = lines.find((line) => line.startsWith('event:'));
    const data = lines
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).trimStart())
      .join('\n');
    if (idLine) lastEventID = Number(idLine.slice(3).trim()) || lastEventID;
    if (!data) return;

    const item = JSON.parse(data) as {
      eventType?: string;
      payload?: {
        delta?: string;
        message?: string;
        output?: RunItem['output'];
        status?: string;
      };
    };
    const eventType = item.eventType || eventLine?.slice(6).trim() || '';
    const payload = item.payload || {};
    if (eventType === 'run.delta' && payload.delta) {
      if (!receivedDelta) {
        messages.value[messageIndex]!.content = '';
        receivedDelta = true;
      }
      messages.value[messageIndex]!.content += payload.delta;
      return;
    }
    if (eventType === 'run.replace') {
      receivedDelta = true;
      messages.value[messageIndex]!.content = String(
        (payload as Record<string, unknown>).content || '',
      );
      return;
    }
    if (eventType === 'run.succeeded') {
      terminal = true;
      if (!receivedDelta) {
        messages.value[messageIndex]!.content = outputText(payload.output)
          || '运行完成，但没有返回文本。';
      }
      return;
    }
    if (eventType === 'run.failed' || eventType === 'run.stopped') {
      terminal = true;
      messages.value[messageIndex]!.content = payload.message
        || `运行状态：${payload.status || eventType.slice(4)}`;
    }
  };

  const handleChunk = (chunk: string) => {
    buffer = (buffer + chunk).replaceAll('\r\n', '\n');
    let boundary = buffer.indexOf('\n\n');
    while (boundary >= 0) {
      handleFrame(buffer.slice(0, boundary));
      buffer = buffer.slice(boundary + 2);
      boundary = buffer.indexOf('\n\n');
    }
  };

  let lastError: unknown;
  for (let attempt = 0; attempt < 4 && !terminal; attempt++) {
    try {
      await streamRunEventsApi(run.id, {
        headers: lastEventID > 0
          ? { 'Last-Event-ID': String(lastEventID) }
          : undefined,
        onMessage: handleChunk,
      });
    } catch (error) {
      lastError = error;
    }
    if (!terminal) await sleep(500 * (attempt + 1));
  }
  if (!terminal && lastError) throw lastError;
  return terminal;
}

async function cancelCurrentRun() {
  if (!currentRunID.value) return;
  await cancelRunApi(currentRunID.value);
}

async function submit() {
  const content = prompt.value.trim();
  const agent = agents.value.find((item) => item.id === selectedAgentID.value);
  if (!content || !agent || sending.value) return;
  messages.value.push({ content, role: 'user' });
  prompt.value = '';
  sending.value = true;
  try {
    const run = await createRunApi({
      agentVersionId: agent.agentVersionId,
      conversationId: conversationID.value || undefined,
      inputs: {},
      query: content,
    });
    currentRunID.value = run.id;
    conversationID.value = run.conversationId || conversationID.value;
    const messageIndex = messages.value.push({ content: '正在思考…', role: 'assistant' }) - 1;
    let streamed = false;
    try {
      streamed = await streamRun(run, messageIndex);
    } catch {
      // 网络中断时使用运行查询兜底，避免丢失最终结果。
    }
    if (!streamed) {
      const completed = await waitForRun(run);
      messages.value[messageIndex]!.content = completed.status === 'succeeded'
        ? outputText(completed.output) || '运行完成，但没有返回文本。'
        : completed.errorMessage || `运行状态：${completed.status}`;
    }
  } finally {
    currentRunID.value = '';
    sending.value = false;
  }
}

onMounted(async () => {
  agents.value = await listAgentsApi();
  const routeAgentID = typeof route.query.agentId === 'string'
    ? route.query.agentId
    : '';
  selectedAgentID.value = agents.value.find((item) => item.id === routeAgentID)?.id
    || agents.value.find((item) => item.enabled && item.configured)?.id
    || '';
});
</script>

<template>
  <Page description="运行任务由 Gin 入队、Worker 调用 Dify，并支持幂等、取消与 SSE 事件接口" title="智能体工作台">
    <ElCard class="mx-auto max-w-5xl">
      <template #header>
        <div class="flex items-center justify-between gap-4">
          <ElSelect v-model="selectedAgentID" class="w-72" placeholder="选择智能体">
            <ElOption
              v-for="agent in agents"
              :key="agent.id"
              :disabled="!agent.enabled || !agent.configured"
              :label="agent.name"
              :value="agent.id"
            />
          </ElSelect>
          <ElTag effect="plain">Dify</ElTag>
        </div>
      </template>
      <div class="mb-5 flex min-h-96 flex-col gap-3">
        <div
          v-for="(message, index) in messages"
          :key="index"
          class="max-w-3xl rounded-lg px-4 py-3"
          :class="message.role === 'user' ? 'ml-auto bg-blue-500 text-white' : 'bg-gray-100 text-gray-800'"
        >
          {{ message.content }}
        </div>
      </div>
      <div class="flex gap-3">
        <ElInput v-model="prompt" :disabled="sending" placeholder="输入消息，按回车发送" @keyup.enter="submit" />
        <ElButton v-if="sending" type="danger" plain @click="cancelCurrentRun">停止</ElButton>
        <ElButton :disabled="!selectedAgentID" :loading="sending" type="primary" @click="submit">发送</ElButton>
      </div>
    </ElCard>
  </Page>
</template>
