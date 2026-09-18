<script setup lang="ts">
import { computed, ref } from "vue";
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

type ItemRow = {
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

const showAllDetail = ref(false);
const expanded = ref<Set<string>>(new Set());

function toggleSource(source: string) {
  const next = new Set(expanded.value);
  if (next.has(source)) next.delete(source);
  else next.add(source);
  expanded.value = next;
}

function toggleAll() {
  showAllDetail.value = !showAllDetail.value;
  if (showAllDetail.value) {
    expanded.value = new Set(ctxRows.value.map((r) => r.source));
  } else {
    expanded.value = new Set();
  }
}

function truncatePreview(s: string, n = 72): string {
  const t = (s || "").replace(/\s+/g, " ").trim();
  if (t.length <= n) return t || "—";
  return t.slice(0, n) + "…";
}

const detailBySource = computed(() => {
  const map = new Map<string, ItemRow[]>();
  for (const i of props.snapshot?.items || []) {
    const source = i.source || "unknown";
    const row: ItemRow = {
      source,
      role: i.role,
      preview: truncatePreview(i.preview || ""),
      tok: i.token_count || 0,
      included: !!i.included,
      excluded: !!i.excluded,
      truncated: !!i.truncated,
      pinned: !!i.pinned,
      reason: i.reason,
    };
    const list = map.get(source);
    if (list) list.push(row);
    else map.set(source, [row]);
  }
  for (const list of map.values()) {
    list.sort((a, b) => b.tok - a.tok);
  }
  return map;
});

/** Aggregate items by source so long sessions stay compact. */
const ctxRows = computed(() => {
  const items = props.snapshot?.items || [];
  const acc = new Map<
    string,
    { source: string; tok: number; count: number; excluded: boolean }
  >();
  for (const i of items) {
    const key = i.source || "unknown";
    const prev = acc.get(key);
    const tok = i.token_count || 0;
    if (prev) {
      prev.tok += tok;
      prev.count += 1;
      if (!i.excluded) prev.excluded = false;
    } else {
      acc.set(key, { source: key, tok, count: 1, excluded: !!i.excluded });
    }
  }
  const rows = [...acc.values()].sort((a, b) => b.tok - a.tok);
  const maxTok = Math.max(1, ...rows.map((r) => r.tok));
  return rows.map((r) => ({
    ...r,
    pct: Math.max(2, Math.round((r.tok / maxTok) * 100)),
  }));
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

    <div class="subhead">
      <span>Context snapshot <span class="hint">by source · click to expand</span></span>
      <button
        v-if="ctxRows.length"
        type="button"
        class="linkish"
        @click="toggleAll"
      >
        {{ showAllDetail ? "折叠全部" : "展开全部" }}
      </button>
    </div>
    <div class="context">
      <div v-if="!ctxRows.length" class="empty">尚无快照</div>
      <template v-else>
        <template v-for="row in ctxRows" :key="row.source">
          <div
            class="ctx-row ctx-row-summary"
            :class="{ excluded: row.excluded, open: expanded.has(row.source) }"
            role="button"
            tabindex="0"
            @click="toggleSource(row.source)"
            @keydown.enter.prevent="toggleSource(row.source)"
            @keydown.space.prevent="toggleSource(row.source)"
          >
            <div class="src">
              <span class="chev">{{ expanded.has(row.source) ? "▾" : "▸" }}</span>
              {{ row.source }}
              <span v-if="row.count > 1" class="cnt">×{{ row.count }}</span>
            </div>
            <div class="ctx-bar"><i :style="{ width: row.pct + '%' }" /></div>
            <div class="tok">{{ row.tok }}</div>
          </div>

          <div v-if="expanded.has(row.source)" class="ctx-detail">
            <div
              v-for="(item, di) in detailBySource.get(row.source) || []"
              :key="row.source + '-' + di"
              class="ctx-detail-row"
              :class="{
                excluded: item.excluded,
                truncated: item.truncated,
              }"
            >
              <div class="d-tok">{{ item.tok }}</div>
              <div class="d-meta">
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
