import { fetchJSON } from "./api";

export function apiMemory() {
  return fetchJSON<{
    file_name?: string;
    path?: string;
    content: string;
    exists?: boolean;
    entries?: number;
    bytes?: number;
    composed_chars?: number;
  }>("/api/memory");
}

export function apiMemoryAdd(entry: string) {
  return fetchJSON<{
    ok: boolean;
    content: string;
    entries?: number;
    composed_chars?: number;
  }>("/api/memory", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ entry }),
  });
}
