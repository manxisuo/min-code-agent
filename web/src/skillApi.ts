import { fetchJSON } from "./api";
import type { SkillDetail, SkillListItem } from "./types";

export function apiSkills() {
  return fetchJSON<{
    skills: SkillListItem[];
    dir: string;
    skills_dir?: string;
    active: string[];
    active_count?: number;
    context_chars?: number;
  }>("/api/skills");
}

export function apiSkill(name: string) {
  return fetchJSON<SkillDetail>(
    "/api/skills/" + encodeURIComponent(name),
  );
}

export function apiSkillActivate(name: string) {
  return fetchJSON<{ ok: boolean; name: string; active: boolean; newly?: boolean }>(
    "/api/skills/" + encodeURIComponent(name) + "/activate",
    { method: "POST" },
  );
}

export function apiSkillDeactivate(name: string) {
  return fetchJSON<{ ok: boolean; name: string; active: boolean }>(
    "/api/skills/" + encodeURIComponent(name) + "/deactivate",
    { method: "POST" },
  );
}
