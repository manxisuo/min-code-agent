<script setup lang="ts">
import { computed } from "vue";
import type { ContextSnapshot, MetricsInfo, RuntimeEvent } from "../types";

const props = defineProps<{
  metrics: MetricsInfo;
  snapshot: ContextSnapshot | null;
  events: RuntimeEvent[];
  timeFmt: (iso?: string) => string;
  eventClass: (type: string) => string;
  shortType: (type: string) => string;
  previewData: (data?: Record<string, unknown>) => string;
}>();

const ctxRows = computed(() => {
  const items = props.snapshot?.items || [];
  const maxTok = Math.max(1, ...items.map((i) => i.token_count || 0));
  return items.map((i) => {
    const tok = i.token_count || 0;
    return {
      source: i.source,
      tok,
      pct: Math.max(2, Math.round((tok / maxTok) * 100)),
      excluded: !!i.excluded,
    };
  });
});

const ctxTotal = computed(() => {
  const s = props.snapshot;
  if (!s) return 0;
  return (s.total_tokens || 0) + (s.tool_tokens || 0);
});
</script>

<template>
  <section class="panel inspector">
    <div class="panel-head">
      <h2>Runtime Inspector</h2>
      <span class="hint">{{ events.length }} events</span>
    </div>

    <div class="metrics">
      <div class="m"><span>LLM</span><b>{{ metrics.llm_calls ?? 0 }}</b></div>
      <div class="m"><span>Tokens</span><b>{{ metrics.total_tokens ?? 0 }}</b></div>
      <div class="m"><span>LLM Time</span><b>{{ metrics.llm_duration_ms ?? 0 }}ms</b></div>
      <div class="m"><span>∥ Batches</span><b>{{ metrics.parallel_batches ?? 0 }}</b></div>
      <div class="m"><span>Errors</span><b>{{ metrics.errors ?? 0 }}</b></div>
    </div>

    <div class="subhead">Context snapshot</div>
    <div class="context">
      <div v-if="!ctxRows.length" class="empty">尚无快照</div>
      <template v-else>
        <div
          v-for="row in ctxRows"
          :key="row.source + row.tok"
          class="ctx-row"
          :class="{ excluded: row.excluded }"
        >
          <div class="src">{{ row.source }}</div>
          <div class="ctx-bar"><i :style="{ width: row.pct + '%' }" /></div>
          <div class="tok">{{ row.tok }}</div>
        </div>
        <div class="ctx-total">
          Total <b>{{ ctxTotal }}</b> / budget {{ snapshot?.budget ?? "-" }}
          · step {{ snapshot?.step ?? "-" }}
          · incl {{ snapshot?.included_count ?? 0 }}
          · excl {{ snapshot?.excluded_count ?? 0 }}
        </div>
      </template>
    </div>

    <div class="subhead">Timeline</div>
    <div class="timeline">
      <div
        v-for="(e, idx) in events"
        :key="e.id || idx"
        class="tl-item"
        :class="eventClass(e.type)"
      >
        <span class="t">{{ timeFmt(e.time) }}</span>
        <span class="ty">{{ shortType(e.type) }}</span>
        <span v-if="previewData(e.data)" class="d">{{ previewData(e.data) }}</span>
      </div>
    </div>
  </section>
</template>
