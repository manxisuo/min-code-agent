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
