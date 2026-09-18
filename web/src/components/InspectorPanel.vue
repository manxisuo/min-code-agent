<script setup lang="ts">
import { computed, ref } from "vue";
import type { ContextItem, ContextSnapshot, MetricsInfo, RuntimeEvent } from "../types";

const props = defineProps<{
  metrics: MetricsInfo;
  snapshot: ContextSnapshot | null;
  events: RuntimeEvent[];
  timeFmt: (iso?: string) => string;
  eventClass: (type: string) => string;
  shortType: (type: string) => string;
  previewData: (data?: Record<string, unknown>) => string;
}>();

type ItemRow = {
  index: number;
  source: string;
  role?: string;
  preview: string;
  tok: number;
  included: boolean;
  excluded: boolean;
  truncated: boolean;
  pinned: boolean;
  reason?: string;
};

type RunRow = {
  id: string;
  source: string;
  count: number;
  tok: number;
  start: number; // 0-based index in snapshot.items
  end: number;
  excluded: boolean;
  items: ItemRow[];
  pct: number;
};

const expanded = ref<Set<string>>(new Set());

function truncatePreview(s: string, n = 72): string {
  const t = (s || "").replace(/\s+/g, " ").trim();
  if (t.length <= n) return t || "—";
  return t.slice(0, n) + "…";
}

function toItemRow(i: ContextItem, index: number): ItemRow {
  return {
    index,
    source: i.source || "unknown",
    role: i.role,
    preview: truncatePreview(i.preview || ""),
    tok: i.token_count || 0,
    included: !!i.included,
    excluded: !!i.excluded,
    truncated: !!i.truncated,
    pinned: !!i.pinned,
    reason: i.reason,
  };
}

/**
 * Run-length fold: merge only consecutive items with the same source.
 * Preserves original context order (history/tool_result may interleave).
 */
const ctxRuns = computed<RunRow[]>(() => {
  const items = props.snapshot?.items || [];
  const runs: Omit<RunRow, "pct">[] = [];
  for (let idx = 0; idx < items.length; idx++) {
    const row = toItemRow(items[idx], idx);
    const last = runs[runs.length - 1];
    if (last && last.source === row.source) {
      last.items.push(row);
      last.tok += row.tok;
      last.count += 1;
      last.end = idx;
      if (!row.excluded) last.excluded = false;
    } else {
      runs.push({
        id: `run-${idx}`,
        source: row.source,
        count: 1,
        tok: row.tok,
        start: idx,
        end: idx,
        excluded: row.excluded,
        items: [row],
      });
    }
  }
  const maxTok = Math.max(1, ...runs.map((r) => r.tok));
  return runs.map((r) => ({
    ...r,
    pct: Math.max(2, Math.round((r.tok / maxTok) * 100)),
  }));
});

function toggleRun(id: string) {
  const next = new Set(expanded.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  expanded.value = next;
}

function toggleAll() {
  if (expanded.value.size > 0) {
    expanded.value = new Set();
    return;
  }
  expanded.value = new Set(ctxRuns.value.map((r) => r.id));
}

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

    <div class="subhead">
      <span>
        Context snapshot
        <span class="hint">in order · consecutive runs folded</span>
      </span>
      <button
        v-if="ctxRuns.length"
        type="button"
        class="linkish"
        @click="toggleAll()"
      >
        {{ expanded.size > 0 ? "折叠全部" : "展开全部" }}
      </button>
    </div>
    <div class="context">
      <div v-if="!ctxRuns.length" class="empty">尚无快照</div>
      <template v-else>
        <template v-for="run in ctxRuns" :key="run.id">
          <div
            class="ctx-row ctx-row-summary"
            :class="{ excluded: run.excluded, open: expanded.has(run.id) }"
            role="button"
            tabindex="0"
            @click="toggleRun(run.id)"
            @keydown.enter.prevent="toggleRun(run.id)"
            @keydown.space.prevent="toggleRun(run.id)"
          >
            <div class="src">
              <span class="chev">{{ expanded.has(run.id) ? "▾" : "▸" }}</span>
              <span class="idx">#{{ run.start }}</span>
              {{ run.source }}
              <span v-if="run.count > 1" class="cnt">×{{ run.count }}</span>
            </div>
            <div class="ctx-bar"><i :style="{ width: run.pct + '%' }" /></div>
            <div class="tok">{{ run.tok }}</div>
          </div>

          <div v-if="expanded.has(run.id)" class="ctx-detail">
            <div
              v-for="item in run.items"
              :key="run.id + '-' + item.index"
              class="ctx-detail-row"
              :class="{ excluded: item.excluded, truncated: item.truncated }"
            >
              <div class="d-tok">{{ item.tok }}</div>
              <div class="d-meta">
                <span class="tag">#{{ item.index }}</span>
                <span v-if="item.role" class="tag">{{ item.role }}</span>
                <span v-if="item.excluded" class="tag warn">excluded</span>
                <span v-else-if="item.truncated" class="tag warn">truncated</span>
                <span v-else class="tag ok">included</span>
                <span v-if="item.pinned" class="tag">pinned</span>
                <span v-if="item.reason" class="reason">{{ item.reason }}</span>
              </div>
              <div class="d-preview" :title="item.preview">{{ item.preview }}</div>
            </div>
          </div>
        </template>

        <div class="ctx-total">
          Total <b>{{ ctxTotal }}</b> / budget {{ snapshot?.budget ?? "-" }}
          · step {{ snapshot?.step ?? "-" }}
          · items {{ snapshot?.items?.length ?? 0 }}
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
        <span class="ty">
          {{ shortType(e.type) }}{{
            e.type === "llm.stream_delta" && Number(e.data?.count || 1) > 1
              ? ` (x${Number(e.data?.count)})`
              : ""
          }}
        </span>
        <span v-if="previewData(e.data)" class="d">{{ previewData(e.data) }}</span>
      </div>
    </div>
  </section>
</template>
