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

func newMemoryApp(t *testing.T, withFile bool) *App {
	t.Helper()
	wsDir := t.TempDir()
	if withFile {
		if err := os.WriteFile(filepath.Join(wsDir, "MEMORY.md"),
			[]byte("# Memory\n\n## 2026-01-01\n\n- Entry point is cmd/mincode\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
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
	var buf bytes.Buffer
	app.out = &buf
	t.Cleanup(func() { _ = app.Close() })
	return app
}

func TestMemoryLoadedAtStartup(t *testing.T) {
	app := newMemoryApp(t, true)
	if !strings.Contains(app.agent.Ctx.Memory(), "cmd/mincode") {
		t.Fatalf("memory = %q", app.agent.Ctx.Memory())
	}
	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Type == observability.EventMemoryRetrieved {
			found = true
		}
	}
	if !found {
		t.Fatal("missing memory.retrieved")
	}
}

func TestMemoryAddCommand(t *testing.T) {
	app := newMemoryApp(t, false)
	var buf bytes.Buffer
	app.out = &buf
	app.handleCommand(context.Background(), "/memory")
	if !strings.Contains(buf.String(), "no project memory") {
		t.Fatalf("empty = %q", buf.String())
	}

	buf.Reset()
	app.handleCommand(context.Background(), "/memory add Tests must stay green with go test ./...")
	if !strings.Contains(buf.String(), "memory updated") {
		t.Fatalf("add = %q", buf.String())
	}
	if !strings.Contains(app.agent.Ctx.Memory(), "go test") {
		t.Fatalf("ctx memory = %q", app.agent.Ctx.Memory())
	}
	if _, err := os.Stat(filepath.Join(app.workspace, "MEMORY.md")); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	app.handleCommand(context.Background(), "/memory")
	if !strings.Contains(buf.String(), "go test") {
		t.Fatalf("show = %q", buf.String())
	}

	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}
	updated := false
	for _, e := range events {
		if e.Type == observability.EventMemoryUpdated {
			updated = true
		}
	}
	if !updated {
		t.Fatal("missing memory.updated")
	}
}

func TestMemoryEntersLLMContext(t *testing.T) {
	app := newMemoryApp(t, true)
	fake := &llm.FakeProvider{Responses: []llm.ChatResponse{{Content: "ok"}}}
	app.agent.Provider = fake
	if err := app.runTurn(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range fake.Requests()[0].Messages {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "cmd/mincode") {
			found = true
		}
	}
	if !found {
		t.Fatal("memory not in system messages")
	}
}
