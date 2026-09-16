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

func newTestAppWithWorkspace(t *testing.T) (*App, string) {
	t.Helper()
	wsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsDir, "hello.txt"), []byte("hello line\n"), 0o644); err != nil {
		t.Fatal(err)
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
	return app, wsDir
}

func TestSingleShotStillWorks(t *testing.T) {
	app, _ := newTestAppWithWorkspace(t)
	if err := app.singleShot(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, e := range events {
		joined += string(e.Type) + ","
	}
	if !strings.Contains(joined, string(observability.EventLLMRequestFinished)) {
		t.Fatalf("events = %s", joined)
	}
}

func TestAgentToolExecutionInCLI(t *testing.T) {
	app, _ := newTestAppWithWorkspace(t)

	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{
				ID:        "1",
				Name:      "read_file",
				Arguments: `{"path":"hello.txt"}`,
			}}},
			{Content: "file contains hello line"},
		},
	}
	app.agent.Provider = fake

	var buf bytes.Buffer
	app.out = &buf
	if err := app.runTurn(context.Background(), "read hello.txt"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "read_file") {
		t.Fatalf("expected tool echo, got %q", out)
	}
	if !strings.Contains(out, "file contains hello line") {
		t.Fatalf("expected final answer, got %q", out)
	}

	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}
	var hasTool, hasState bool
	for _, e := range events {
		if e.Type == observability.EventToolFinished {
			hasTool = true
		}
		if e.Type == observability.EventAgentStateChanged {
			hasState = true
		}
	}
	if !hasTool || !hasState {
		t.Fatalf("missing tool/state events")
	}
}

func TestTimelineCommand(t *testing.T) {
	app, _ := newTestAppWithWorkspace(t)
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{
				ID: "1", Name: "list_dir", Arguments: `{"path":"."}`,
			}}},
			{Content: "done"},
		},
	}
	app.agent.Provider = fake
	if err := app.runTurn(context.Background(), "list"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	app.out = &buf
	quit := app.handleCommand("/timeline")
	if quit {
		t.Fatal("timeline should not quit")
	}
	out := buf.String()
	if !strings.Contains(out, "Tool Start") || !strings.Contains(out, "LLM Response") {
		t.Fatalf("timeline = %q", out)
	}
}
