import { fetchJSON } from "./api";
import type { PendingPermission } from "./types";

export function apiPendingPermissions() {
  return fetchJSON<{ pending: PendingPermission[] }>(
    "/api/permissions/pending",
  );
}

export function apiDecidePermission(id: string, allow: boolean) {
  return fetchJSON<{ ok: boolean; id: string; allow: boolean }>(
    "/api/permissions/" + encodeURIComponent(id),
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ allow }),
    },
  );
}
