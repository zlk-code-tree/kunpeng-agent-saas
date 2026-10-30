<script lang="ts" setup>
import { useTemplateRef } from 'vue';

import { Settings } from '@vben/icons';

import { VbenIconButton } from '@vben-core/shadcn-ui';

import Preferences from './preferences.vue';

const emit = defineEmits<{ clearPreferencesAndLogout: [] }>();
const preferencesRef =
  useTemplateRef<InstanceType<typeof Preferences>>('preferencesRef');

function clearPreferencesAndLogout() {
  emit('clearPreferencesAndLogout');
}

// 自定义触发按钮会替换 Preferences 内置按钮，因此必须显式调用抽屉的公开 open 方法。
function openPreferences() {
  preferencesRef.value?.open();
}
</script>
<template>
  <Preferences
    ref="preferencesRef"
    @clear-preferences-and-logout="clearPreferencesAndLogout"
  >
    <VbenIconButton
      class="hover:animate-[shrink_0.3s_ease-in-out]"
      @click="openPreferences"
    >
      <Settings class="size-4 text-foreground" />
    </VbenIconButton>
  </Preferences>
</template>
