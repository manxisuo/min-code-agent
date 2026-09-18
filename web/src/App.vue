<script setup lang="ts">
import { computed, ref } from "vue";
import ChatPanel from "./components/ChatPanel.vue";
import ExperimentPanel from "./components/ExperimentPanel.vue";
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

const view = ref<"inspector" | "experiments">("inspector");

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
        <nav class="view-tabs">
          <button
            type="button"
            :class="{ active: view === 'inspector' }"
            @click="view = 'inspector'"
          >
            Inspector
          </button>
          <button
            type="button"
            :class="{ active: view === 'experiments' }"
            @click="view = 'experiments'"
          >
            Experiments
          </button>
        </nav>
        <span class="pill" :data-state="stateLabel">{{ stateLabel }}</span>
        <button type="button" :disabled="!running" @click="cancel()">Cancel</button>
      </div>
    </header>

    <main class="layout" :class="{ 'layout-full': view === 'experiments' }">
      <template v-if="view === 'inspector'">
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
      </template>
      <ExperimentPanel v-else />
    </main>
  </div>
</template>
