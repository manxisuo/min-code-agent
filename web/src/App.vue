<script setup lang="ts">
import { computed, ref } from "vue";
import ChatPanel from "./components/ChatPanel.vue";
import ExperimentPanel from "./components/ExperimentPanel.vue";
import InspectorPanel from "./components/InspectorPanel.vue";
import InstructionsPanel from "./components/InstructionsPanel.vue";
import MemoryPanel from "./components/MemoryPanel.vue";
import PlanPanel from "./components/PlanPanel.vue";
import PermissionBar from "./components/PermissionBar.vue";
import SessionsPanel from "./components/SessionsPanel.vue";
import SkillsPanel from "./components/SkillsPanel.vue";
import { useInspector } from "./composables/useInspector";
import { useTheme } from "./theme";

const { theme, toggle } = useTheme();

const {
  messages,
  events,
  session,
  metrics,
  snapshot,
  input,
  send,
  cancel,
  loadSession,
  eventClass,
  shortType,
  previewData,
  fmtTime,
} = useInspector();

const view = ref<
  | "inspector"
  | "plan"
  | "skills"
  | "instructions"
  | "memory"
  | "sessions"
  | "experiments"
>("inspector");

const stateLabel = computed(() => session.value?.state || "IDLE");
const running = computed(() => !!session.value?.running);
const meta = computed(() => {
  const s = session.value;
  if (!s) return "connecting…";
  return `${s.session_id} · ${s.provider}/${s.model} · ${s.workspace}`;
});
const themeLabel = computed(() => (theme.value === "dark" ? "Light" : "Dark"));
const exportNote = ref("");
const exportBusy = ref(false);

async function exportMarkdown(download: boolean) {
  if (download) {
    window.open("/api/export/download", "_blank");
    return;
  }
  exportBusy.value = true;
  exportNote.value = "";
  try {
    const res = await fetch("/api/export", { method: "POST" });
    const data = (await res.json().catch(() => ({}))) as {
      ok?: boolean;
      rel?: string;
      path?: string;
      error?: string;
    };
    if (!res.ok || !data.ok) {
      exportNote.value = data.error || res.statusText;
      return;
    }
    exportNote.value = `已导出 ${data.rel || data.path}`;
  } catch (e) {
    exportNote.value = String((e as Error).message || e);
  } finally {
    exportBusy.value = false;
  }
}

function onInput(v: string) {
  input.value = v;
}

function onSend() {
  void send();
}

async function onSwitchSession(id: string) {
  await loadSession(id);
  view.value = "inspector";
}
</script>

<template>
  <div class="app-shell">
    <PermissionBar />
    <header class="top">
      <div class="brand">
        <span class="logo">MC</span>
        <div>
          <strong>MinCode Inspector</strong>
          <div class="meta">{{ exportNote || meta }}</div>
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
            :class="{ active: view === 'plan' }"
            @click="view = 'plan'"
          >
            Plan
          </button>
          <button
            type="button"
            :class="{ active: view === 'skills' }"
            @click="view = 'skills'"
          >
            Skills
          </button>
          <button
            type="button"
            :class="{ active: view === 'instructions' }"
            @click="view = 'instructions'"
          >
            Instructions
          </button>
          <button
            type="button"
            :class="{ active: view === 'memory' }"
            @click="view = 'memory'"
          >
            Memory
          </button>
          <button
            type="button"
            :class="{ active: view === 'sessions' }"
            @click="view = 'sessions'"
          >
            Sessions
          </button>
          <button
            type="button"
            :class="{ active: view === 'experiments' }"
            @click="view = 'experiments'"
          >
            Experiments
          </button>
        </nav>
        <button type="button" class="theme-btn" :title="'切换到' + themeLabel + '主题'" @click="toggle()">
          {{ theme === "dark" ? "🌙" : "☀" }} {{ themeLabel }}
        </button>
        <button
          type="button"
          class="theme-btn"
          title="导出到 workspace/exports/*.md"
          :disabled="exportBusy"
          @click="exportMarkdown(false)"
        >
          Export
        </button>
        <button
          type="button"
          class="theme-btn"
          title="浏览器下载 Markdown"
          @click="exportMarkdown(true)"
        >
          ↓MD
        </button>
        <span class="pill" :data-state="stateLabel">{{ stateLabel }}</span>
        <button type="button" :disabled="!running" @click="cancel()">Cancel</button>
      </div>
    </header>

    <main
      class="layout"
      :class="{
        'layout-full':
          view === 'experiments' ||
          view === 'plan' ||
          view === 'skills' ||
          view === 'instructions' ||
          view === 'memory' ||
          view === 'sessions',
      }"
    >
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
      <PlanPanel v-else-if="view === 'plan'" />
      <SkillsPanel v-else-if="view === 'skills'" />
      <InstructionsPanel v-else-if="view === 'instructions'" />
      <MemoryPanel v-else-if="view === 'memory'" />
      <SessionsPanel
        v-else-if="view === 'sessions'"
        :load-session="onSwitchSession"
      />
      <ExperimentPanel v-else />
    </main>
  </div>
</template>
