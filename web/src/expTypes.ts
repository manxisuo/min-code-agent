export interface Stat {
  min: number;
  median: number;
  max: number;
}

export interface ExperimentAggregate {
  name: string;
  runs: number;
  successes: number;
  avg_steps?: number;
  avg_tool_calls?: number;
  avg_llm_calls?: number;
  avg_input_tokens?: number;
  avg_output_tokens?: number;
  avg_total_tokens?: number;
  avg_duration_ms?: number;
  avg_compactions?: number;
  total_failures?: number;
  models?: string;
  tasks?: string;
  steps_stat?: Stat;
  tool_calls_stat?: Stat;
  total_tokens_stat?: Stat;
  duration_ms_stat?: Stat;
}

export interface ExperimentRun {
  run_id: string;
  experiment: string;
  task?: string;
  provider?: string;
  model?: string;
  success: boolean;
  error?: string;
  steps: number;
  tool_calls: number;
  llm_calls: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  duration_ms: number;
  final_preview?: string;
}

export interface ExperimentDetail {
  aggregate: ExperimentAggregate;
  runs: ExperimentRun[];
}
