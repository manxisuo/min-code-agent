<script setup lang="ts">
import { computed } from "vue";
import ChatPanel from "./components/ChatPanel.vue";
import InspectorPanel from "./components/InspectorPanel.vue";
import { useInspector } from "./composables/useInspector";

const {
  messages,
  events,
  session,
  metrics,
  snapshot,
  input,
  send,
  cancel,
  eventClass,
  shortType,
  previewData,
  fmtTime,
} = useInspector();

const stateLabel = computed(() => session.value?.state || "IDLE");
const running = computed(() => !!session.value?.running);
const meta = computed(() => {
  const s = session.value;
  if (!s) return "connecting…";
  return `${s.session_id} · ${s.provider}/${s.model} · ${s.workspace}`;
});

function onInput(v: string) {
  input.value = v;
}

function onSend() {
  void send();
}
</script>

<template>
  <div class="app-shell">
    <header class="top">
      <div class="brand">
        <span class="logo">MC</span>
        <div>
          <strong>MinCode Inspector</strong>
          <div class="meta">{{ meta }}</div>
        </div>
      </div>
      <div class="top-right">
        <span class="pill" :data-state="stateLabel">{{ stateLabel }}</span>
        <button type="button" :disabled="!running" @click="cancel()">Cancel</button>
      </div>
    </header>

    <main class="layout">
      <ChatPanel
        :messages="messages"
        :input="input"
        :disabled="running"
        @update:input="onInput"
        @send="onSend"
      />
      <InspectorPanel
        :metrics="metrics"
        :snapshot="snapshot"
        :events="events"
        :time-fmt="fmtTime"
        :event-class="eventClass"
        :short-type="shortType"
        :preview-data="previewData"
      />
    </main>
  </div>
</template>
