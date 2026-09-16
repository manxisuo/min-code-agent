package config

// Config is the runtime configuration for mincode.
type Config struct {
	Provider ProviderConfig `yaml:"provider"`
	Agent    AgentConfig    `yaml:"agent"`
	Trace    TraceConfig    `yaml:"trace"`
}

// ProviderConfig selects and configures an LLM provider.
type ProviderConfig struct {
	// Type is "openai-compatible" or "fake".
	Type        string  `yaml:"type"`
	BaseURL     string  `yaml:"base_url"`
	APIKey      string  `yaml:"api_key"`
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
	TimeoutSec  int     `yaml:"timeout_sec"`
}

// AgentConfig holds basic agent-loop limits.
type AgentConfig struct {
	MaxSteps     int    `yaml:"max_steps"`
	SystemPrompt string `yaml:"system_prompt"`
	// TokenBudget caps estimated prompt tokens per LLM call (0 = default 32000).
	TokenBudget int `yaml:"token_budget"`
}

// TraceConfig controls where JSONL traces are written.
type TraceConfig struct {
	Dir string `yaml:"dir"`
}

// Default returns a sensible default configuration.
func Default() Config {
	return Config{
		Provider: ProviderConfig{
			Type:        "openai-compatible",
			BaseURL:     "https://api.openai.com/v1",
			Model:       "gpt-4o-mini",
			Temperature: 0.7,
			TimeoutSec:  120,
		},
		Agent: AgentConfig{
			MaxSteps:     30,
			SystemPrompt: DefaultSystemPrompt,
			TokenBudget:  32000,
		},
		Trace: TraceConfig{
			Dir: ".mincode/traces",
		},
	}
}

// DefaultSystemPrompt is the minimal system prompt for the agent.
const DefaultSystemPrompt = `You are Min Code Agent, a coding assistant working inside a workspace.

You have read-only tools to explore the repository:
- list_dir: list files in a directory
- glob: find files by pattern
- grep: search file contents with regexp
- read_file: read a file (optionally a line range)

When asked to analyze a project, use these tools to inspect real files before answering.
Cite concrete file paths in your answers. Prefer small, targeted tool calls.
When you have enough information, reply with a final answer and no tool calls.
`
