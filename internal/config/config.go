package config

// Config is the runtime configuration for mincode.
type Config struct {
	Provider ProviderConfig `yaml:"provider"`
	Agent    AgentConfig    `yaml:"agent"`
	Memory   MemoryConfig   `yaml:"memory"`
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
	// CompressAt triggers history compaction when estimated history tokens exceed this (0 disables).
	CompressAt int `yaml:"compress_at"`
}

// TraceConfig controls where JSONL traces are written.
type TraceConfig struct {
	Dir string `yaml:"dir"`
}

// MemoryConfig controls cross-session MEMORY.md behavior.
type MemoryConfig struct {
	// AutoExtract, when true, asks the LLM after each successful turn whether
	// a durable cross-session fact should be written (still requires approval).
	AutoExtract bool `yaml:"auto_extract"`
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
			CompressAt:   18000,
		},
		Trace: TraceConfig{
			Dir: ".mincode/traces",
		},
	}
}

// DefaultSystemPrompt is the minimal system prompt for the agent.
const DefaultSystemPrompt = `You are Min Code Agent, a coding assistant working inside a workspace.

You have tools to explore and modify the repository:
- list_dir / glob / grep / read_file: inspect code
- write_file / edit_file: create or modify files (requires user approval)
- shell: run commands (go test, git status allow; destructive commands denied)

When asked to analyze a project, use read-only tools first and cite concrete file paths.
When asked to change code, make the smallest correct edit; prefer edit_file for existing files.
When asked to validate, run tests via shell and fix failures if needed.
Write operations will ask the user for permission — propose clear, reviewable changes.
Prefer dedicated tools (read_file, list_dir, glob, grep) over shell for inspecting files.
When you have enough information, reply with a final answer and no tool calls.
`

// PlatformShellHint returns OS-specific shell guidance for the system prompt.
func PlatformShellHint(goos string) string {
	switch goos {
	case "windows":
		return `
Shell runs via cmd.exe on Windows. Do NOT use Unix-only commands (wc, head, tail, cat, ls, grep, sed, awk, which).
Use instead: dir, type, findstr, where, powershell -Command if needed.
Example: type docs\go-intro.md  or  dir docs
`
	default:
		return `
Shell runs via /bin/sh. Prefer portable commands; avoid destructive operations.
`
	}
}
