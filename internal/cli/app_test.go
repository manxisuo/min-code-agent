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

func newTestApp(t *testing.T) *App {
	t.Helper()
	traceDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "mincode.yaml")
	yaml := "provider:\n  type: fake\n  model: fake-model\ntrace:\n  dir: " + filepath.ToSlash(traceDir) + "\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	app, err := NewApp(Options{ConfigPath: cfgPath, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	app.out = &buf
	t.Cleanup(func() { _ = app.Close() })
	return app
}

func TestSingleShotFakeProvider(t *testing.T) {
	app := newTestApp(t)
	resp, err := app.chat(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content == "" {
		t.Fatal("empty content")
	}

	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}

	var types []observability.EventType
	for _, e := range events {
		types = append(types, e.Type)
	}
	joined := strings.Join(mapToStrings(types), ",")
	if !strings.Contains(joined, string(observability.EventSessionCreated)) {
		t.Fatalf("missing session.created in %s", joined)
	}
	if !strings.Contains(joined, string(observability.EventLLMRequestStarted)) {
		t.Fatalf("missing llm.request_started in %s", joined)
	}
	if !strings.Contains(joined, string(observability.EventLLMRequestFinished)) {
		t.Fatalf("missing llm.request_finished in %s", joined)
	}

	m := app.metrics.Snapshot()
	if m.LLMCalls != 1 {
		t.Fatalf("llm calls = %d", m.LLMCalls)
	}
	if m.TotalTokens <= 0 {
		t.Fatalf("total tokens = %d", m.TotalTokens)
	}

	// History should be system + user + assistant.
	if len(app.history) != 3 {
		t.Fatalf("history len = %d", len(app.history))
	}
	if app.history[1].Role != llm.RoleUser || app.history[2].Role != llm.RoleAssistant {
		t.Fatalf("history roles = %v %v", app.history[1].Role, app.history[2].Role)
	}
}

func TestChatFailureDropsUserMessage(t *testing.T) {
	app := newTestApp(t)
	// Replace provider with one that fails.
	fake := llm.NewFakeProvider("m", "x")
	fake.Err = context.DeadlineExceeded
	app.provider = fake

	_, err := app.chat(context.Background(), "boom")
	if err == nil {
		t.Fatal("expected error")
	}
	// system prompt only
	if len(app.history) != 1 {
		t.Fatalf("history = %d, want 1", len(app.history))
	}
}

func TestHandleCommandTraceAndMetrics(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.chat(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	app.out = &buf

	quit := app.handleCommand("/metrics")
	if quit {
		t.Fatal("metrics should not quit")
	}
	if !strings.Contains(buf.String(), "LLM Calls") {
		t.Fatalf("metrics output = %q", buf.String())
	}

	buf.Reset()
	quit = app.handleCommand("/trace 5")
	if quit {
		t.Fatal("trace should not quit")
	}
	if !strings.Contains(buf.String(), string(observability.EventLLMRequestFinished)) {
		t.Fatalf("trace output = %q", buf.String())
	}

	buf.Reset()
	if quit := app.handleCommand("/exit"); !quit {
		t.Fatal("exit should quit")
	}
}

func TestHandleCommandClear(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.chat(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	app.out = &buf
	app.handleCommand("/clear")
	if len(app.history) != 1 {
		t.Fatalf("history = %d", len(app.history))
	}
	if app.history[0].Role != llm.RoleSystem {
		t.Fatalf("role = %s", app.history[0].Role)
	}
}

func mapToStrings(in []observability.EventType) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}
