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

// AgentConfig holds basic agent-loop limits (used starting Phase 2).
type AgentConfig struct {
	MaxSteps     int    `yaml:"max_steps"`
	SystemPrompt string `yaml:"system_prompt"`
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
		},
		Trace: TraceConfig{
			Dir: ".mincode/traces",
		},
	}
}

// DefaultSystemPrompt is the minimal system prompt for Phase 1 chat.
const DefaultSystemPrompt = `You are Min Code Agent, a helpful coding assistant.

Answer clearly and concisely. When discussing code, prefer concrete examples.
`
