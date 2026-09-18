<script setup lang="ts">
import { computed } from "vue";
import { renderMarkdown } from "../markdown";

const props = defineProps<{
  content: string;
  /** system notes stay plain; user/assistant render as markdown */
  plain?: boolean;
}>();

const html = computed(() =>
  props.plain ? "" : renderMarkdown(props.content),
);
</script>

<template>
  <div v-if="plain" class="body plain-text">{{ content }}</div>
  <!-- eslint-disable-next-line vue/no-v-html — sanitized via DOMPurify -->
  <div v-else class="body md-body" v-html="html" />
</template>
