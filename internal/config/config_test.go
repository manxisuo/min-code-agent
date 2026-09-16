package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Provider.Type != "openai-compatible" {
		t.Fatalf("type = %q", cfg.Provider.Type)
	}
	if cfg.Trace.Dir != ".mincode/traces" {
		t.Fatalf("trace dir = %q", cfg.Trace.Dir)
	}
	if cfg.Agent.MaxSteps != 30 {
		t.Fatalf("max steps = %d", cfg.Agent.MaxSteps)
	}
}

func TestLoadMissingExplicitPath(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error for missing explicit config")
	}
}

func TestLoadYAMLAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mincode.yaml")
	content := `
provider:
  type: fake
  model: scripted-model
  base_url: http://localhost:1234/v1
trace:
  dir: custom/traces
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MINCODE_MODEL", "env-model")
	t.Setenv("MINCODE_API_KEY", "sk-test")
	t.Setenv("OPENAI_API_KEY", "should-not-win")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Type != "fake" {
		t.Fatalf("type = %q", cfg.Provider.Type)
	}
	if cfg.Provider.Model != "env-model" {
		t.Fatalf("model = %q, env should override yaml", cfg.Provider.Model)
	}
	if cfg.Provider.APIKey != "sk-test" {
		t.Fatalf("api key = %q", cfg.Provider.APIKey)
	}
	if cfg.Provider.BaseURL != "http://localhost:1234/v1" {
		t.Fatalf("base url = %q", cfg.Provider.BaseURL)
	}
	if cfg.Trace.Dir != "custom/traces" {
		t.Fatalf("trace dir = %q", cfg.Trace.Dir)
	}
}

func TestLoadMissingFileInCwdIsOK(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model == "" {
		t.Fatal("expected default model")
	}
}

func TestTracePath(t *testing.T) {
	p := TracePath("traces", "abc")
	if filepath.Base(p) != "abc.jsonl" {
		t.Fatalf("path = %q", p)
	}
}
