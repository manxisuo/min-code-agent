package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileCreateAndOverwrite(t *testing.T) {
	ws := newWS(t)
	tool := &WriteFile{WS: ws}

	res, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "pkg/new.go", "content": "package pkg\n",
	}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if res.Meta["operation"] != "created" {
		t.Fatalf("op = %v", res.Meta["operation"])
	}
	data, _ := os.ReadFile(filepath.Join(ws.Root(), "pkg", "new.go"))
	if string(data) != "package pkg\n" {
		t.Fatalf("content = %q", data)
	}

	res, err = tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "pkg/new.go", "content": "package pkg // v2\n",
	}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if res.Meta["operation"] != "overwrote" {
		t.Fatalf("op = %v", res.Meta["operation"])
	}
}

func TestWriteFileEscapeRejected(t *testing.T) {
	ws := newWS(t)
	tool := &WriteFile{WS: ws}
	res, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "../evil.txt", "content": "x",
	}))
	if !res.IsError {
		t.Fatal("escape should fail")
	}
}

func TestEditFileUniqueReplace(t *testing.T) {
	ws := newWS(t)
	write(t, ws, "a.go", "package main\n\nfunc Hello() {}\n")
	tool := &EditFile{WS: ws}

	res, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "a.go", "old_text": "func Hello() {}", "new_text": "func Hello() string { return \"hi\" }",
	}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	data, _ := os.ReadFile(filepath.Join(ws.Root(), "a.go"))
	if !strings.Contains(string(data), "Hello() string") {
		t.Fatalf("file = %q", data)
	}
	diff, _ := res.Meta["diff"].(string)
	if !strings.Contains(diff, "- ") || !strings.Contains(diff, "+ ") {
		t.Fatalf("diff = %q", diff)
	}
}

func TestEditFileZeroAndMultipleMatches(t *testing.T) {
	ws := newWS(t)
	write(t, ws, "a.go", "foo\nfoo\nbar\n")
	tool := &EditFile{WS: ws}

	res, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "a.go", "old_text": "missing", "new_text": "x",
	}))
	if !res.IsError || !strings.Contains(res.Content, "0 matches") {
		t.Fatalf("zero: %+v", res)
	}

	res, _ = tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "a.go", "old_text": "foo", "new_text": "x",
	}))
	if !res.IsError || !strings.Contains(res.Content, "2 times") {
		t.Fatalf("multi: %+v", res)
	}
}

func TestEditFileEscape(t *testing.T) {
	ws := newWS(t)
	tool := &EditFile{WS: ws}
	res, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "../x", "old_text": "a", "new_text": "b",
	}))
	if !res.IsError {
		t.Fatal("escape should fail")
	}
}

func TestUnifiedDiff(t *testing.T) {
	oldT := "line1\nline2\nline3\nline4\n"
	newT := "line1\nline2 changed\nline3\nline4\n"
	d := UnifiedDiff("f.txt", oldT, newT, 1)
	if !strings.Contains(d, "- line2") || !strings.Contains(d, "+ line2 changed") {
		t.Fatalf("diff = %s", d)
	}
	if !strings.Contains(d, "  line1") {
		t.Fatalf("missing context: %s", d)
	}
}
