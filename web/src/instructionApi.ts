import { fetchJSON } from "./api";

export interface InstructionItem {
  rel_path: string;
  rel_dir: string;
  path: string;
  bytes: number;
  preview?: string;
  content?: string;
}

export function apiInstructions() {
  return fetchJSON<{
    instructions: InstructionItem[];
    file_name?: string;
    workspace?: string;
    count?: number;
    composed_chars?: number;
    composed?: string;
  }>("/api/instructions");
}

export function apiInstructionsAll() {
  return fetchJSON<{
    instructions: InstructionItem[];
    file_name?: string;
    count?: number;
    composed_chars?: number;
  }>("/api/instructions/all");
}

export function apiInstructionsReload() {
  return fetchJSON<{ ok: boolean; count?: number; composed_chars?: number; root?: boolean }>(
    "/api/instructions/reload",
    { method: "POST" },
  );
}
