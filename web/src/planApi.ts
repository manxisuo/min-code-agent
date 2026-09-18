import type { Plan } from "./types";
import { fetchJSON } from "./api";

export function apiPlan() {
  return fetchJSON<{ plan: Plan | null }>("/api/plan");
}

export function apiPlanDraft(goal: string) {
  return fetchJSON<{ plan: Plan }>("/api/plan", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ goal }),
  });
}

export function apiPlanApprove() {
  return fetchJSON<{ ok: boolean; plan: Plan }>("/api/plan/approve", {
    method: "POST",
  });
}

export function apiPlanReject() {
  return fetchJSON<{ ok: boolean }>("/api/plan/reject", {
    method: "POST",
  });
}

export function apiPlanCancel() {
  return fetchJSON<{ ok: boolean; plan: Plan }>("/api/plan/cancel", {
    method: "POST",
  });
}
