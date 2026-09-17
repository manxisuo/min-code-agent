package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	shellDefaultTimeout = 60 * time.Second
	shellMaxOutput      = 256 * 1024
)

// Shell runs a command inside the workspace with timeout and cancellation.
type Shell struct {
	WS *Workspace
	// Timeout overrides the default when > 0.
	Timeout time.Duration
}

func (t *Shell) Name() string { return "shell" }

func (t *Shell) Description() string {
	return "Run a shell command in the workspace working directory (Windows: cmd.exe; Unix: /bin/sh). Capture stdout/stderr/exit code. Prefer read_file/list_dir/grep over shell for reading files. Do not use path traversal (../) or absolute paths outside the workspace — those are denied or require approval. On Windows do not use wc/head/cat/ls — use dir/type/findstr."
}

func (t *Shell) Schema() JSONSchema {
	return JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Command line to execute",
			},
			"working_directory": map[string]any{
				"type":        "string",
				"description": "Relative working directory (default: workspace root)",
			},
			"timeout_sec": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds (default 60)",
			},
		},
		"required": []string{"command"},
	}
}

type shellArgs struct {
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
	TimeoutSec       int    `json:"timeout_sec"`
}

func (t *Shell) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var args shellArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(args.Command) == "" {
		return Result{Content: "command is required", IsError: true}, nil
	}

	cwd := t.WS.Root()
	if args.WorkingDirectory != "" {
		abs, err := t.WS.Resolve(args.WorkingDirectory)
		if err != nil {
			return Result{Content: err.Error(), IsError: true}, nil
		}
		cwd = abs
	}

	timeout := t.Timeout
	if timeout <= 0 {
		timeout = shellDefaultTimeout
	}
	if args.TimeoutSec > 0 {
		timeout = time.Duration(args.TimeoutSec) * time.Second
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "cmd.exe", "/C", args.Command)
	} else {
		cmd = exec.CommandContext(runCtx, "/bin/sh", "-c", args.Command)
	}
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, max: shellMaxOutput}
	cmd.Stderr = &limitWriter{w: &stderr, max: shellMaxOutput}

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	exitCode := 0
	timedOut := false
	if runCtx.Err() == context.DeadlineExceeded {
		timedOut = true
		exitCode = -1
	} else if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			return Result{
				Content: fmt.Sprintf("run error: %v", err),
				IsError: true,
			}, nil
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "exit_code=%d duration=%s\n", exitCode, elapsed.Round(time.Millisecond))
	if timedOut {
		fmt.Fprintf(&b, "TIMEOUT after %s\n", timeout)
	}
	if stdout.Len() > 0 {
		fmt.Fprintf(&b, "--- stdout ---\n%s\n", truncateUTF8(stdout.String(), 32*1024))
	}
	if stderr.Len() > 0 {
		fmt.Fprintf(&b, "--- stderr ---\n%s\n", truncateUTF8(stderr.String(), 32*1024))
	}

	isErr := exitCode != 0 || timedOut
	return Result{
		Content: b.String(),
		IsError: isErr,
		Meta: map[string]any{
			"command":     args.Command,
			"exit_code":   exitCode,
			"duration_ms": elapsed.Milliseconds(),
			"stdout_len":  stdout.Len(),
			"stderr_len":  stderr.Len(),
			"timed_out":   timedOut,
			"cwd":         cwd,
		},
	}, nil
}

type limitWriter struct {
	w   *bytes.Buffer
	max int
	n   int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n >= l.max {
		return len(p), nil // discard overflow, keep command running
	}
	room := l.max - l.n
	if len(p) > room {
		_, _ = l.w.Write(p[:room])
		l.n = l.max
		return len(p), nil
	}
	n, err := l.w.Write(p)
	l.n += n
	return len(p), err
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "\n... (truncated)\n"
}
