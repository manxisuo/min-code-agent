export interface RuntimeEvent {
  id?: string;
  time?: string;
  step?: number;
  type: string;
  data?: Record<string, unknown>;
}

export interface ContextItem {
  source: string;
  role?: string;
  preview?: string;
  token_count?: number;
  included?: boolean;
  excluded?: boolean;
  truncated?: boolean;
  pinned?: boolean;
  reason?: string;
  tool_call_id?: string;
}

export interface ContextSnapshot {
  step?: number;
  items?: ContextItem[];
  total_tokens?: number;
  tool_tokens?: number;
  budget?: number;
  included_count?: number;
  excluded_count?: number;
  truncated_count?: number;
}

export interface SessionInfo {
  session_id: string;
  workspace: string;
  provider: string;
  model: string;
  state: string;
  running: boolean;
  last_error?: string;
  turn?: {
    final?: string;
    steps?: number;
    tool_calls?: number;
  };
  snapshot?: ContextSnapshot | null;
}

export interface MetricsInfo {
  llm_calls?: number;
  errors?: number;
  input_tokens?: number;
  output_tokens?: number;
  total_tokens?: number;
  llm_duration_ms?: number;
  parallel_batches?: number;
  parallel_tool_calls?: number;
}

export interface ChatMessage {
  id: string;
  role: "user" | "assistant" | "system";
  text: string;
}
