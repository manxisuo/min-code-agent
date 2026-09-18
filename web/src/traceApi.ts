import type { RuntimeEvent } from "./types";
import { fetchJSON } from "./api";

export interface TraceListItem {
  id: string;
  path: string;
  size: number;
  mod_time: string;
  is_current: boolean;
  source?: string;
}

export function apiTraces() {
  return fetchJSON<{
    dir: string;
    dirs?: string[];
    traces: TraceListItem[];
    session: string;
  }>("/api/traces");
}

export function apiTrace(id: string, opts?: { type?: string; limit?: number }) {
  const params = new URLSearchParams();
  if (opts?.type) params.set("type", opts.type);
  if (opts?.limit) params.set("limit", String(opts.limit));
  const q = params.toString();
  return fetchJSON<{
    id: string;
    path: string;
    total: number;
    shown: number;
    events: RuntimeEvent[];
  }>(`/api/traces/${encodeURIComponent(id)}${q ? `?${q}` : ""}`);
}
