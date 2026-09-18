import type { MetricsInfo, SessionInfo } from "./types";

export async function fetchJSON<T>(url: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(url, opts);
  const data = (await res.json().catch(() => ({}))) as T & { error?: string };
  if (!res.ok) {
    const err = (data as { error?: string }).error || res.statusText;
    throw new Error(err);
  }
  return data;
}

export function apiSession() {
  return fetchJSON<SessionInfo>("/api/session");
}

export function apiMetrics() {
  return fetchJSON<MetricsInfo>("/api/metrics");
}

export function apiChat(message: string) {
  return fetchJSON<{ ok: boolean; running: boolean }>("/api/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message }),
  });
}

export function apiCancel() {
  return fetchJSON<{ ok: boolean; cancelled: boolean }>("/api/cancel", {
    method: "POST",
  });
}
