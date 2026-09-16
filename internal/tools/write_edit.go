package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	writeFileMaxBytes = 1 << 20 // 1MB
)

// WriteFile creates or overwrites a file inside the workspace.
type WriteFile struct {
	WS *Workspace
}

func (t *WriteFile) Name() string { return "write_file" }

func (t *WriteFile) Description() string {
	return "Create or overwrite a file in the workspace. Parent directories are created as needed."
}

func (t *WriteFile) Schema() JSONSchema {
	return JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to workspace root",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full file content to write",
			},
		},
		"required": []string{"path", "content"},
	}
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteFile) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var args writeFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if args.Path == "" {
		return Result{Content: "path is required", IsError: true}, nil
	}
	if len(args.Content) > writeFileMaxBytes {
		return Result{Content: fmt.Sprintf("content too large (%d bytes, max %d)", len(args.Content), writeFileMaxBytes), IsError: true}, nil
	}

	abs, err := t.WS.Resolve(args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return Result{Content: fmt.Sprintf("%s is a directory", args.Path), IsError: true}, nil
	}

	existed := false
	var prevSize int64
	if info, err := os.Stat(abs); err == nil {
		existed = true
		prevSize = info.Size()
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return Result{Content: fmt.Sprintf("mkdir: %v", err), IsError: true}, nil
	}
	if err := os.WriteFile(abs, []byte(args.Content), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("write %s: %v", args.Path, err), IsError: true}, nil
	}

	rel := args.Path
	op := "created"
	if existed {
		op = "overwrote"
	}
	return Result{
		Content: fmt.Sprintf("%s %s (%d bytes)", op, rel, len(args.Content)),
		Meta: map[string]any{
			"path":       rel,
			"operation":  op,
			"bytes":      len(args.Content),
			"prev_bytes": prevSize,
			"existed":    existed,
		},
	}, nil
}

// PreviewEdit computes the unified diff for an edit without writing the file.
// Used by the permission approver so the user can review before approving.
func PreviewEdit(ws *Workspace, path, oldText, newText string) (string, error) {
	abs, err := ws.Resolve(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return "", fmt.Errorf("old_text not found")
	}
	if count > 1 {
		return "", fmt.Errorf("old_text matched %d times", count)
	}
	updated := strings.Replace(content, oldText, newText, 1)
	return UnifiedDiff(path, content, updated, 3), nil
}

// PreviewWrite returns a short "new file" style preview for write_file.
func PreviewWrite(path, content string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n", path)
	lines := splitKeepEmpty(content)
	max := 30
	if len(lines) < max {
		max = len(lines)
	}
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
	for i := 0; i < max; i++ {
		b.WriteString("+ " + lines[i] + "\n")
	}
	if len(lines) > max {
		fmt.Fprintf(&b, "+ ... (%d more lines)\n", len(lines)-max)
	}
	return b.String()
}

const (
	editFileMaxOld = 256 * 1024
	editFileMaxNew = 256 * 1024
)

// EditFile replaces a unique exact substring in a workspace file.
type EditFile struct {
	WS *Workspace
}

func (t *EditFile) Name() string { return "edit_file" }

func (t *EditFile) Description() string {
	return "Replace an exact unique substring in a file. old_text must match exactly once; 0 or multiple matches fail."
}

func (t *EditFile) Schema() JSONSchema {
	return JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to workspace root",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "Exact text to replace (must appear exactly once)",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "Replacement text",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

type editFileArgs struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

func (t *EditFile) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var args editFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if args.Path == "" || args.OldText == "" {
		return Result{Content: "path and old_text are required", IsError: true}, nil
	}
	if args.OldText == args.NewText {
		return Result{Content: "old_text and new_text are identical", IsError: true}, nil
	}
	if len(args.OldText) > editFileMaxOld || len(args.NewText) > editFileMaxNew {
		return Result{Content: "old_text/new_text too large", IsError: true}, nil
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
		return Result{Content: fmt.Sprintf("%s is a directory", args.Path), IsError: true}, nil
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{Content: fmt.Sprintf("read %s: %v", args.Path, err), IsError: true}, nil
	}
	content := string(data)
	// Normalize to \n for matching; preserve original style on write if possible.
	// Phase 4: operate on raw bytes-as-string to avoid surprising CRLF rewrites.
	count := strings.Count(content, args.OldText)
	if count == 0 {
		return Result{Content: "old_text not found (0 matches)", IsError: true}, nil
	}
	if count > 1 {
		return Result{Content: fmt.Sprintf("old_text matched %d times; must be unique", count), IsError: true}, nil
	}

	updated := strings.Replace(content, args.OldText, args.NewText, 1)
	if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("write %s: %v", args.Path, err), IsError: true}, nil
	}

	diff := UnifiedDiff(args.Path, content, updated, 3)
	return Result{
		Content: fmt.Sprintf("updated %s\n\n%s", args.Path, diff),
		Meta: map[string]any{
			"path":      args.Path,
			"operation": "edit",
			"matches":   1,
			"diff":      diff,
			"bytes":     len(updated),
		},
	}, nil
}
