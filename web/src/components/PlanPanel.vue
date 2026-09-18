<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  apiPlan,
  apiPlanApprove,
  apiPlanCancel,
  apiPlanDraft,
  apiPlanReject,
} from "../planApi";
import type { Plan } from "../types";

const plan = ref<Plan | null>(null);
const goal = ref("");
const error = ref("");
const busy = ref(false);

const progress = computed(() => {
  if (!plan.value) return { done: 0, total: 0 };
  const steps = plan.value.steps || [];
  const done = steps.filter(
    (s) => s.status === "done" || s.status === "skipped",
  ).length;
  return { done, total: steps.length };
});

const canApprove = computed(() => plan.value?.status === "draft");
const canReject = computed(
  () => plan.value?.status === "draft" || plan.value?.status === "approved",
);
const canCancel = computed(
  () =>
    plan.value?.status === "running" || plan.value?.status === "approved",
);

async function refresh() {
  try {
    const data = await apiPlan();
    plan.value = data.plan || null;
  } catch (e) {
    error.value = String((e as Error).message || e);
  }
}

async function draft() {
  const g = goal.value.trim();
  if (!g) {
    error.value = "请填写 plan goal";
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    const data = await apiPlanDraft(g);
    plan.value = data.plan;
  } catch (e) {
    error.value = String((e as Error).message || e);
  } finally {
    busy.value = false;
  }
}

async function approve() {
  busy.value = true;
  error.value = "";
  try {
    await apiPlanApprove();
    await refresh();
  } catch (e) {
    error.value = String((e as Error).message || e);
  } finally {
    busy.value = false;
  }
}

async function reject() {
  busy.value = true;
  error.value = "";
  try {
    await apiPlanReject();
    plan.value = null;
  } catch (e) {
    error.value = String((e as Error).message || e);
  } finally {
    busy.value = false;
  }
}

async function cancel() {
  busy.value = true;
  error.value = "";
  try {
    const data = await apiPlanCancel();
    plan.value = data.plan || plan.value;
  } catch (e) {
    error.value = String((e as Error).message || e);
  } finally {
    busy.value = false;
  }
}

function mark(st: string): string {
  switch (st) {
    case "done":
      return "✓";
    case "running":
      return "›";
    case "failed":
      return "✗";
    case "cancelled":
      return "·";
    case "skipped":
      return "-";
    default:
      return "○";
  }
}

defineExpose({ refresh });

let poll: number | null = null;
onMounted(() => {
  void refresh();
  poll = window.setInterval(() => {
    if (!busy.value) void refresh();
  }, 2000);
});
onBeforeUnmount(() => {
  if (poll != null) window.clearInterval(poll);
});
</script>

<template>
  <section class="panel plan-panel">
    <div class="panel-head">
      <h2>Plan</h2>
      <button type="button" class="linkish" :disabled="busy" @click="refresh()">
        Refresh
      </button>
    </div>

    <div class="plan-compose">
      <input
        v-model="goal"
        class="tl-input plan-goal"
        placeholder="例如：分析项目入口并补一个 /health 测试"
        :disabled="busy"
        @keyup.enter="draft()"
      />
      <button type="button" class="primary" :disabled="busy" @click="draft()">
        Draft
      </button>
    </div>

    <div v-if="error" class="exp-error">{{ error }}</div>

    <div v-if="!plan" class="empty plan-empty">
      尚无计划。输入 goal 后点 Draft，模型会生成步骤列表；确认后 Approve 执行。
    </div>

    <div v-else class="plan-body">
      <div class="plan-meta">
        <div class="plan-goal-line">
          <b>{{ plan.id }}</b>
          <span class="pill plan-status" :data-status="plan.status">{{ plan.status }}</span>
        </div>
        <div class="plan-goal-text">{{ plan.goal }}</div>
        <div class="plan-progress">
          steps {{ progress.done }}/{{ progress.total }}
          <template v-if="plan.current"> · current #{{ plan.current }}</template>
        </div>
      </div>

      <div class="plan-actions">
        <button
          v-if="canApprove"
          type="button"
          class="primary"
          :disabled="busy"
          @click="approve()"
        >
          Approve & Run
        </button>
        <button v-if="canReject" type="button" :disabled="busy" @click="reject()">
          Reject
        </button>
        <button v-if="canCancel" type="button" :disabled="busy" @click="cancel()">
          Cancel
        </button>
      </div>

      <ol class="plan-steps">
        <li
          v-for="step in plan.steps"
          :key="step.index"
          class="plan-step"
          :data-status="step.status"
        >
          <span class="step-mark">{{ mark(step.status) }}</span>
          <div class="step-body">
            <div class="step-title">{{ step.index }}. {{ step.title }}</div>
            <div v-if="step.result" class="step-note">{{ step.result }}</div>
            <div v-if="step.error" class="step-err">{{ step.error }}</div>
          </div>
        </li>
      </ol>
    </div>
  </section>
</template>
