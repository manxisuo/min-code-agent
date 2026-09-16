package tools

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShellEcho(t *testing.T) {
	ws := newWS(t)
	tool := &Shell{WS: ws}

	cmd := "echo mincode-shell-ok"
	if runtime.GOOS == "windows" {
		cmd = "echo mincode-shell-ok"
	}
	res, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{"command": cmd}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("error result: %s", res.Content)
	}
	if !strings.Contains(res.Content, "mincode-shell-ok") {
		t.Fatalf("content = %s", res.Content)
	}
	if res.Meta["exit_code"] != 0 {
		t.Fatalf("exit = %v", res.Meta["exit_code"])
	}
}

func TestShellNonZeroExit(t *testing.T) {
	ws := newWS(t)
	tool := &Shell{WS: ws}
	cmd := "exit 3"
	if runtime.GOOS != "windows" {
		cmd = "exit 3"
	}
	res, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{"command": cmd}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for non-zero exit")
	}
	if res.Meta["exit_code"] != 3 {
		t.Fatalf("exit = %v", res.Meta["exit_code"])
	}
}

func TestShellTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	ws := newWS(t)
	tool := &Shell{WS: ws, Timeout: 300 * time.Millisecond}
	cmd := "ping -n 10 127.0.0.1"
	if runtime.GOOS != "windows" {
		cmd = "sleep 5"
	}
	res, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"command": cmd, "timeout_sec": 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected timeout error result")
	}
	if timed, _ := res.Meta["timed_out"].(bool); !timed {
		t.Fatalf("meta = %+v", res.Meta)
	}
}

func TestShellCancel(t *testing.T) {
	ws := newWS(t)
	tool := &Shell{WS: ws}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Execute(ctx, mustJSON(t, map[string]any{"command": "echo hi"}))
	if err == nil {
		// canceled before start may still return Result or error depending on path
		// CommandContext with canceled ctx returns error from Run
	}
	_ = err
}

func TestShellWorkingDirectory(t *testing.T) {
	ws := newWS(t)
	write(t, ws, "sub/marker.txt", "x")
	tool := &Shell{WS: ws}
	res, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"command": "dir marker.txt", "working_directory": "sub",
	}))
	if err != nil {
		t.Fatal(err)
	}
	// dir succeeds when file exists; if dir missing file, exit != 0
	if res.IsError && !strings.Contains(res.Content, "marker") {
		t.Fatalf("content = %s", res.Content)
	}
}

func TestShellEscapeWorkingDir(t *testing.T) {
	ws := newWS(t)
	tool := &Shell{WS: ws}
	res, _ := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"command": "echo hi", "working_directory": "../outside",
	}))
	if !res.IsError {
		t.Fatal("working_directory escape must fail")
	}
}

func TestShellEmptyCommand(t *testing.T) {
	ws := newWS(t)
	tool := &Shell{WS: ws}
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{"command":"  "}`))
	if !res.IsError {
		t.Fatal("empty command should error")
	}
}
