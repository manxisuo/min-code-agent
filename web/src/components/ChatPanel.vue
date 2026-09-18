<script setup lang="ts">
import type { ChatMessage } from "../types";
import MdText from "./MdText.vue";

const props = defineProps<{
  messages: ChatMessage[];
  input: string;
  disabled?: boolean;
}>();

const emit = defineEmits<{
  "update:input": [value: string];
  send: [];
}>();

function onSubmit() {
  if (!props.disabled) emit("send");
}
</script>

<template>
  <section class="panel chat">
    <div class="panel-head">
      <h2>Conversation</h2>
      <span class="hint">Local Web · Markdown</span>
    </div>
    <div class="messages" aria-live="polite">
      <div v-for="m in messages" :key="m.id" class="msg" :class="m.role">
        <div class="role">{{ m.role }}</div>
        <MdText :content="m.text" :plain="m.role === 'system'" />
      </div>
    </div>
    <form class="composer" @submit.prevent="onSubmit">
      <textarea
        rows="3"
        placeholder="输入消息（支持 Markdown），例如：**分析**这个项目的入口。"
        :value="input"
        :disabled="disabled"
        @input="emit('update:input', ($event.target as HTMLTextAreaElement).value)"
      />
      <button class="primary" type="submit" :disabled="disabled">Send</button>
    </form>
  </section>
</template>
