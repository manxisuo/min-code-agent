package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mincode/mincode/internal/llm"
	"github.com/mincode/mincode/internal/observability"
	"github.com/mincode/mincode/internal/permission"
	"github.com/mincode/mincode/internal/tools"
)

// Phase 10.5 hardening: sandbox + policy + context survival.

func TestHardeningFilePathEscapeDenied(t *testing.T) {
	wsDir := t.TempDir()
	ws, err := tools.NewWorkspace(wsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"../../etc/passwd",
		"../outside.txt",
		"/etc/passwd",
		`C:\Windows\win.ini`,
	} {
		if _, err := ws.Resolve(p); err == nil {
			t.Fatalf("path %q must escape-fail", p)
		}
	}
}

func TestHardeningShellPolicy(t *testing.T) {
	cases := []struct {
		cmd  string
		want permission.Level
	}{
		{"go test ./...", permission.Allow},
		{"git status", permission.Allow},
		{"rm -rf .", permission.Deny},
		{"git reset --hard", permission.Deny},
		{"git clean -fd", permission.Deny},
		{"cat ../../secret", permission.Deny},
		{"cd ..", permission.Deny},
		{"cat /etc/passwd", permission.Ask},
	}
	for _, c := range cases {
		if got := permission.ClassifyShell(c.cmd); got != c.want {
			t.Fatalf("%q => %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestHardeningInstructionsAndSkillsSurviveTurns(t *testing.T) {
	wsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsDir, "AGENTS.md"), []byte("ALWAYS-CITE-PATHS"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wsDir, "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "skills", "demo", "SKILL.md"),
		[]byte("# Demo\n\nSKILL-MARKER\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	traceDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "mincode.yaml")
	yaml := "provider:\n  type: fake\n  model: fake-model\nagent:\n  token_budget: 4000\n  compress_at: 1500\ntrace:\n  dir: " + filepath.ToSlash(traceDir) + "\n"
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
	app.handleCommand(context.Background(), "/skill demo")

	fake := &llm.FakeProvider{}
	for i := 0; i < 6; i++ {
		fake.Responses = append(fake.Responses, llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{
				ID:        fmt.Sprintf("call_%d", i),
				Name:      "read_file",
				Arguments: fmt.Sprintf(`{"path":"hello.txt","start_line":%d}`, i+1),
			}},
		})
	}
	fake.Responses = append(fake.Responses, llm.ChatResponse{Content: strings.Repeat("pad ", 200)})
	app.agent.Provider = fake

	for i := 0; i < 3; i++ {
		if err := app.runTurn(context.Background(), strings.Repeat("please explain more ", 40)); err != nil {
			t.Fatal(err)
		}
	}

	if !strings.Contains(app.agent.Ctx.Instructions(), "ALWAYS-CITE-PATHS") {
		t.Fatalf("instructions lost: %q", app.agent.Ctx.Instructions())
	}
	if !strings.Contains(app.agent.Ctx.Skills(), "SKILL-MARKER") {
		t.Fatalf("skills lost: %q", app.agent.Ctx.Skills())
	}
	if app.agent.Ctx.LastSnapshot() == nil {
		t.Fatal("no snapshot")
	}
}

func TestHardeningReadFileOutsideWorkspaceRecoverable(t *testing.T) {
	app, _ := newTestAppWithWorkspace(t)
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{
				ID: "1", Name: "read_file", Arguments: `{"path":"../secret.txt"}`,
			}}},
			{Content: "could not read outside workspace"},
		},
	}
	app.agent.Provider = fake
	var buf bytes.Buffer
	app.out = &buf
	if err := app.runTurn(context.Background(), "read secret"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "could not read outside workspace") {
		t.Fatalf("out = %q", buf.String())
	}
	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}
	var toolFinished bool
	for _, e := range events {
		if e.Type == observability.EventToolFinished {
			toolFinished = true
		}
	}
	if !toolFinished {
		t.Fatal("missing tool.finished for recoverable error")
	}
}
