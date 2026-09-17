package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mincode/mincode/internal/llm"
	"github.com/mincode/mincode/internal/observability"
)

func TestMemoryAddToolViaAgent(t *testing.T) {
	wsDir := t.TempDir()
	traceDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "mincode.yaml")
	yaml := "provider:\n  type: fake\n  model: fake-model\ntrace:\n  dir: " + filepath.ToSlash(traceDir) + "\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Options{ConfigPath: cfgPath, Workspace: wsDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	var buf bytes.Buffer
	app.out = &buf

	// Auto-approve permission prompts.
	if ap, ok := app.agent.Approver.(*StdinApprover); ok {
		ap.AutoYes = true
	}

	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{
				ID:        "1",
				Name:      "memory_add",
				Arguments: `{"entry":"Tests live under internal/*/_test.go"}`,
			}}},
			{Content: "I saved that fact."},
		},
	}
	app.agent.Provider = fake

	if err := app.runTurn(context.Background(), "remember where tests live"); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(app.agent.Ctx.Memory(), "internal/*/_test.go") {
		t.Fatalf("memory = %q", app.agent.Ctx.Memory())
	}
	if _, err := os.Stat(filepath.Join(wsDir, "MEMORY.md")); err != nil {
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
		t.Fatal("missing memory.updated from tool")
	}
}

func TestMemoryAddToolRegistered(t *testing.T) {
	app, _ := newTestAppWithWorkspace(t)
	names := app.toolNames()
	found := false
	for _, n := range names {
		if n == "memory_add" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tools = %v", names)
	}
}
