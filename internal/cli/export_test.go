package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mincode/mincode/internal/ctxmgr"
	"github.com/mincode/mincode/internal/llm"
)

func TestRenderSessionMarkdown(t *testing.T) {
	entries := []ctxmgr.ExportedEntry{
		{Msg: llm.Message{Role: llm.RoleUser, Content: "hello world"}},
		{Msg: llm.Message{
			Role:    llm.RoleAssistant,
			Content: "hi there",
			ToolCalls: []llm.ToolCall{
				{ID: "c1", Name: "read_file", Arguments: `{"path":"a.go"}`},
			},
		}},
		{Msg: llm.Message{Role: llm.RoleTool, Content: "package a\n", ToolCallID: "c1"}},
		{Msg: llm.Message{Role: llm.RoleAssistant, Content: "final answer"}},
	}
	md := renderSessionMarkdown(sessionExportMeta{
		SessionID: "sess-1",
		Workspace: "/ws",
		Provider:  "fake",
		Model:     "m",
	}, entries)

	for _, want := range []string{
		"# Session sess-1",
		"### User",
		"hello world",
		"### Assistant",
		"#### Tool calls",
		"read_file",
		"#### Tool result",
		"final answer",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q in:\n%s", want, md)
		}
	}
}

func TestExportCommandWritesFile(t *testing.T) {
	app, _ := newTestAppWithWorkspace(t)
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{
				ID: "1", Name: "read_file", Arguments: `{"path":"hello.txt"}`,
			}}},
			{Content: "The file says hello line."},
		},
	}
	app.agent.Provider = fake
	var buf bytes.Buffer
	app.out = &buf
	if err := app.runTurn(context.Background(), "read hello"); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	app.handleCommand(context.Background(), "/export")
	out := buf.String()
	if !strings.Contains(out, "exported") {
		t.Fatalf("export out = %q", out)
	}

	path := filepath.Join(app.workspace, "exports", app.sessionID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	md := string(data)
	if !strings.Contains(md, "### User") || !strings.Contains(md, "The file says hello line") {
		t.Fatalf("markdown = %s", md)
	}
	if !strings.Contains(md, "hello.txt") {
		t.Fatalf("tool call missing: %s", md)
	}
}

func TestExportCommandCustomPath(t *testing.T) {
	app, _ := newTestAppWithWorkspace(t)
	fake := llm.NewFakeProvider("m", "short answer")
	app.agent.Provider = fake
	var buf bytes.Buffer
	app.out = &buf
	if err := app.runTurn(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	app.handleCommand(context.Background(), "/export notes/session.md")
	if !strings.Contains(buf.String(), "notes/session.md") {
		t.Fatalf("out = %q", buf.String())
	}
	if _, err := os.Stat(filepath.Join(app.workspace, "notes", "session.md")); err != nil {
		t.Fatal(err)
	}
}
