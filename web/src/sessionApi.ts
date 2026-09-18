import { fetchJSON } from "./api";
import type { SessionListItem, SessionLoadResult } from "./types";

export function apiSessions() {
  return fetchJSON<{
    sessions: SessionListItem[];
    current?: string;
    process_id?: string;
    sessions_dir?: string;
  }>("/api/sessions");
}

export function apiSessionLoad(id: string) {
  return fetchJSON<SessionLoadResult>(
    "/api/sessions/" + encodeURIComponent(id) + "/load",
    { method: "POST" },
  );
}
