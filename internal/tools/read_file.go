package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	readFileMaxLines = 2000
	readFileMaxBytes = 512 * 1024
)

// ReadFile reads a text file with optional 1-based inclusive line range.
type ReadFile struct {
	WS *Workspace
}

func (t *ReadFile) Name() string { return "read_file" }

func (t *ReadFile) Description() string {
	return "Read a text file from the workspace. Optionally specify start_line and end_line (1-based, inclusive). Output is truncated if too large."
}

func (t *ReadFile) Schema() JSONSchema {
	return JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to workspace root",
			},
			"start_line": map[string]any{
				"type":        "integer",
				"description": "First line to read (1-based, inclusive)",
			},
			"end_line": map[string]any{
				"type":        "integer",
				"description": "Last line to read (1-based, inclusive)",
			},
		},
		"required": []string{"path"},
	}
}

type readFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func (t *ReadFile) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var args readFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if args.Path == "" {
		return Result{Content: "path is required", IsError: true}, nil
	}

	abs, err := t.WS.Resolve(args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}

	info, err := os.Stat(abs)
	if err != nil {
		return Result{Content: fmt.Sprintf("stat %s: %v", args.Path, err), IsError: true}, nil
	}
	if info.IsDir() {
		return Result{Content: fmt.Sprintf("%s is a directory; use list_dir", args.Path), IsError: true}, nil
	}
	if info.Size() > readFileMaxBytes {
		// Still allow ranged reads of huge files.
		if args.StartLine <= 0 && args.EndLine <= 0 {
			return Result{
				Content: fmt.Sprintf("file %s is %d bytes (limit %d); specify start_line/end_line", args.Path, info.Size(), readFileMaxBytes),
				IsError: true,
			}, nil
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{Content: fmt.Sprintf("read %s: %v", args.Path, err), IsError: true}, nil
	}
	if isBinary(data) {
		return Result{Content: fmt.Sprintf("%s looks like a binary file", args.Path), IsError: true}, nil
	}

	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	// Drop trailing empty element from final newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)

	start := 1
	end := total
	if args.StartLine > 0 {
		start = args.StartLine
	}
	if args.EndLine > 0 {
		end = args.EndLine
	}
	if start < 1 {
		start = 1
	}
	if end > total {
		end = total
	}
	if start > total {
		return Result{Content: fmt.Sprintf("start_line %d beyond EOF (%d lines)", start, total), IsError: true}, nil
	}
	if end < start {
		return Result{Content: fmt.Sprintf("end_line %d < start_line %d", end, start), IsError: true}, nil
	}

	truncated := false
	if end-start+1 > readFileMaxLines {
		end = start + readFileMaxLines - 1
		truncated = true
	}

	var b strings.Builder
	width := len(fmt.Sprintf("%d", end))
	for i := start; i <= end; i++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		fmt.Fprintf(&b, "%*d| %s\n", width, i, lines[i-1])
	}
	if truncated {
		fmt.Fprintf(&b, "... (truncated, showing lines %d-%d of %d)\n", start, end, total)
	} else if end < total {
		fmt.Fprintf(&b, "... (%d more lines)\n", total-end)
	}

	return Result{
		Content: b.String(),
		Meta: map[string]any{
			"path":        args.Path,
			"bytes":       len(data),
			"total_lines": total,
			"start_line":  start,
			"end_line":    end,
			"truncated":   truncated,
		},
	}, nil
}

func isBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}
