package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manxisuo/mincode/internal/llm"
	"github.com/manxisuo/mincode/internal/observability"
)

func newAutoMemApp(t *testing.T, autoExtract bool) *App {
	t.Helper()
	wsDir := t.TempDir()
	traceDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "mincode.yaml")
	auto := "false"
	if autoExtract {
		auto = "true"
	}
	yaml := "provider:\n  type: fake\n  model: fake-model\nmemory:\n  auto_extract: " + auto + "\ntrace:\n  dir: " + filepath.ToSlash(traceDir) + "\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Options{ConfigPath: cfgPath, Workspace: wsDir})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	app.out = &buf
	t.Cleanup(func() { _ = app.Close() })
	return app
}

func TestMemoryAutoExtractDisabledByDefault(t *testing.T) {
	app := newAutoMemApp(t, false)
	if app.cfg.Memory.AutoExtract {
		t.Fatal("auto_extract should default false")
	}
	fake := &llm.FakeProvider{Responses: []llm.ChatResponse{{Content: "ok answer"}}}
	app.agent.Provider = fake
	app.provider = fake
	if err := app.runTurn(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if fake.Calls() != 1 {
		t.Fatalf("calls = %d, want 1 (no extract)", fake.Calls())
	}
}

func TestMemoryAutoExtractNONE(t *testing.T) {
	app := newAutoMemApp(t, true)
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{Content: "final answer about the task"},
			{Content: "NONE"},
		},
	}
	app.agent.Provider = fake
	app.provider = fake
	if ap, ok := app.agent.Approver.(*StdinApprover); ok {
		ap.AutoYes = true
	}
	if err := app.runTurn(context.Background(), "do something temporary"); err != nil {
		t.Fatal(err)
	}
	if fake.Calls() != 2 {
		t.Fatalf("calls = %d, want 2", fake.Calls())
	}
	if app.agent.Ctx.Memory() != "" {
		t.Fatalf("memory should stay empty: %q", app.agent.Ctx.Memory())
	}
}

func TestMemoryAutoExtractSavesFact(t *testing.T) {
	app := newAutoMemApp(t, true)
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{Content: "The CLI entry is cmd/mincode/main.go."},
			{Content: "CLI entry is cmd/mincode/main.go"},
		},
	}
	app.agent.Provider = fake
	app.provider = fake
	if ap, ok := app.agent.Approver.(*StdinApprover); ok {
		ap.AutoYes = true
	}
	var buf bytes.Buffer
	app.out = &buf

	if err := app.runTurn(context.Background(), "where is the entry?"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(app.agent.Ctx.Memory(), "cmd/mincode/main.go") {
		t.Fatalf("memory = %q", app.agent.Ctx.Memory())
	}
	if !strings.Contains(buf.String(), "memory") {
		t.Fatalf("out = %q", buf.String())
	}
	if _, err := os.Stat(filepath.Join(app.workspace, "MEMORY.md")); err != nil {
		t.Fatal(err)
	}

	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}
	var updated bool
	for _, e := range events {
		if e.Type == observability.EventMemoryUpdated {
			updated = true
		}
	}
	if !updated {
		t.Fatal("missing memory.updated")
	}
}

func TestMemoryAutoExtractSkipsSecret(t *testing.T) {
	app := newAutoMemApp(t, true)
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{Content: "configured the key"},
			{Content: "api_key=sk-should-not-save"},
		},
	}
	app.agent.Provider = fake
	app.provider = fake
	if ap, ok := app.agent.Approver.(*StdinApprover); ok {
		ap.AutoYes = true
	}
	if err := app.runTurn(context.Background(), "set key"); err != nil {
		t.Fatal(err)
	}
	if app.agent.Ctx.Memory() != "" {
		t.Fatalf("secret leaked into memory: %q", app.agent.Ctx.Memory())
	}
}
