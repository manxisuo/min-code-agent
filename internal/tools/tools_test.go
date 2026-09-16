package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newWS(t *testing.T) *Workspace {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func write(t *testing.T, ws *Workspace, rel, content string) string {
	t.Helper()
	abs := filepath.Join(ws.Root(), rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return abs
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWorkspaceRejectsTraversal(t *testing.T) {
	ws := newWS(t)
	cases := []string{
		"../outside.txt",
		"../../etc/passwd",
		"..\\..\\windows\\system32",
		"/etc/passwd",
		"/Windows/System32/drivers/etc/hosts",
	}
	for _, c := range cases {
		if _, err := ws.Resolve(c); err == nil {
			t.Fatalf("expected reject for %q", c)
		}
	}
}

func TestWorkspaceAllowsInside(t *testing.T) {
	ws := newWS(t)
	write(t, ws, "a/b.txt", "hi")
	p, err := ws.Resolve("a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, "b.txt") {
		t.Fatalf("path = %s", p)
	}
	if _, err := ws.Resolve("."); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceSymlinkEscape(t *testing.T) {
	ws := newWS(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws.Root(), "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if _, err := ws.Resolve("escape/secret.txt"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestReadFile(t *testing.T) {
	ws := newWS(t)
	write(t, ws, "hello.txt", "line1\nline2\nline3\n")
	tool := &ReadFile{WS: ws}

	res, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{"path": "hello.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "1| line1") || !strings.Contains(res.Content, "2| line2") {
		t.Fatalf("content = %q", res.Content)
	}

	res, err = tool.Execute(context.Background(), mustJSON(t, map[string]any{"path": "hello.txt", "start_line": 2, "end_line": 2}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Content, "line1") || !strings.Contains(res.Content, "2| line2") {
		t.Fatalf("ranged content = %q", res.Content)
	}
}

func TestReadFileMissingAndEscape(t *testing.T) {
	ws := newWS(t)
	tool := &ReadFile{WS: ws}

	res, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{"path": "nope.txt"}))
	if !res.IsError {
		t.Fatal("missing file should error")
	}
	res, _ = tool.Execute(context.Background(), mustJSON(t, map[string]any{"path": "../x"}))
	if !res.IsError {
		t.Fatal("escape should error")
	}
}

func TestReadFileInvalidArgs(t *testing.T) {
	ws := newWS(t)
	tool := &ReadFile{WS: ws}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("invalid json should be tool error")
	}
}

func TestListDir(t *testing.T) {
	ws := newWS(t)
	write(t, ws, "sub/a.txt", "x")
	write(t, ws, "b.txt", "y")
	tool := &ListDir{WS: ws}

	res, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{"path": "."}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Content, "dir  sub/") || !strings.Contains(res.Content, "file b.txt") {
		t.Fatalf("content = %q", res.Content)
	}
}

func TestGlob(t *testing.T) {
	ws := newWS(t)
	write(t, ws, "a.go", "package a\n")
	write(t, ws, "sub/b.go", "package b\n")
	write(t, ws, "sub/c.txt", "c\n")
	write(t, ws, "deep/nested/d.go", "package d\n")
	tool := &Glob{WS: ws}

	res, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "**/*.go"}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	for _, want := range []string{"a.go", "sub/b.go", "deep/nested/d.go"} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("missing %s in %q", want, res.Content)
		}
	}
	if strings.Contains(res.Content, "c.txt") {
		t.Fatalf("unexpected c.txt in %q", res.Content)
	}

	res, err = tool.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "*.go", "path": "sub"}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Content, "b.go") || strings.Contains(res.Content, "a.go") {
		t.Fatalf("content = %q", res.Content)
	}
}

func TestGrep(t *testing.T) {
	ws := newWS(t)
	write(t, ws, "main.go", "package main\nfunc Hello() {}\n")
	write(t, ws, "util.go", "package main\n// Hello helper\n")
	write(t, ws, "readme.md", "Hello world\n")
	tool := &Grep{WS: ws}

	res, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"pattern":      "Hello",
		"file_pattern": "*.go",
	}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Content, "main.go:2:") {
		t.Fatalf("content = %q", res.Content)
	}
	if strings.Contains(res.Content, "readme.md") {
		t.Fatalf("file_pattern not applied: %q", res.Content)
	}
}

func TestGrepInvalidRegex(t *testing.T) {
	ws := newWS(t)
	tool := &Grep{WS: ws}
	res, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "("}))
	if !res.IsError {
		t.Fatal("invalid regexp should error")
	}
}

func TestRegistryUnknownTool(t *testing.T) {
	r := NewRegistry()
	res, err := r.Execute(context.Background(), "nope", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("unknown tool should be error result")
	}
}

func TestMatchSegment(t *testing.T) {
	cases := []struct {
		pat, s string
		want   bool
	}{
		{"*.go", "a.go", true},
		{"*.go", "a.txt", false},
		{"foo*", "foobar", true},
		{"foo", "foobar", false},
		{"?", "a", true},
		{"*", "anything", true},
	}
	for _, c := range cases {
		if got := matchSegment(c.pat, c.s); got != c.want {
			t.Fatalf("match(%q,%q)=%v want %v", c.pat, c.s, got, c.want)
		}
	}
}
