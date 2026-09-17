package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const listDirMaxEntries = 200

// ListDir lists immediate children of a directory.
type ListDir struct {
	WS *Workspace
}

func (t *ListDir) Name() string { return "list_dir" }

func (t *ListDir) Description() string {
	return "List files and subdirectories in a workspace directory (non-recursive)."
}

func (t *ListDir) Schema() JSONSchema {
	return JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path relative to workspace root (default: .)",
			},
		},
	}
}

type listDirArgs struct {
	Path string `json:"path"`
}

func (t *ListDir) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var args listDirArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}

	abs, err := t.WS.Resolve(args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}

	info, err := os.Stat(abs)
	if err != nil {
		return Result{Content: fmt.Sprintf("stat %s: %v", args.Path, err), IsError: true}, nil
	}
	if !info.IsDir() {
		return Result{Content: fmt.Sprintf("%s is a file; use read_file", args.Path), IsError: true}, nil
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return Result{Content: fmt.Sprintf("read dir %s: %v", args.Path, err), IsError: true}, nil
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	var b strings.Builder
	shown := 0
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if shown >= listDirMaxEntries {
			fmt.Fprintf(&b, "... (%d more entries)\n", len(entries)-shown)
			break
		}
		if e.IsDir() {
			fmt.Fprintf(&b, "dir  %s/\n", e.Name())
		} else {
			size := int64(0)
			if fi, err := e.Info(); err == nil {
				size = fi.Size()
			}
			fmt.Fprintf(&b, "file %s  (%d bytes)\n", e.Name(), size)
		}
		shown++
	}
	if shown == 0 {
		b.WriteString("(empty directory)\n")
	}

	metaPath := args.Path
	if metaPath == "" {
		metaPath = "."
	}
	return Result{
		Content: b.String(),
		Meta: map[string]any{
			"path":    metaPath,
			"entries": len(entries),
		},
	}, nil
}
