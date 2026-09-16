package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	globMaxResults = 200
	globSkipDirs   = ".git;.mincode;node_modules;vendor;__pycache__"
)

// Glob finds files matching a glob pattern under a workspace path.
// Supports * and ** (recursive).
type Glob struct {
	WS *Workspace
}

func (t *Glob) Name() string { return "glob" }

func (t *Glob) Description() string {
	return `Find files by glob pattern under a workspace path. Supports * and ** (recursive). Examples: **/*.go, internal/**/*.go, *.md`
}

func (t *Glob) Schema() JSONSchema {
	return JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern, e.g. **/*.go",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search from (default: workspace root)",
			},
		},
		"required": []string{"pattern"},
	}
}

type globArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func (t *Glob) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var args globArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if args.Pattern == "" {
		return Result{Content: "pattern is required", IsError: true}, nil
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
		return Result{Content: fmt.Sprintf("%s is not a directory", args.Path), IsError: true}, nil
	}

	pattern := filepath.ToSlash(args.Pattern)
	skip := map[string]bool{}
	for _, d := range strings.Split(globSkipDirs, ";") {
		skip[d] = true
	}

	var matches []string
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if skip[name] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if matchGlob(pattern, relSlash) || matchGlob(pattern, name) {
			matches = append(matches, relSlash)
			if len(matches) > globMaxResults {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil && err != fs.SkipAll {
		return Result{Content: fmt.Sprintf("walk: %v", err), IsError: true}, nil
	}

	sort.Strings(matches)
	truncated := false
	if len(matches) > globMaxResults {
		matches = matches[:globMaxResults]
		truncated = true
	}

	if len(matches) == 0 {
		return Result{
			Content: "no matches",
			Meta:    map[string]any{"pattern": args.Pattern, "count": 0},
		}, nil
	}

	var b strings.Builder
	for _, m := range matches {
		b.WriteString(m)
		b.WriteByte('\n')
	}
	if truncated {
		fmt.Fprintf(&b, "... (truncated at %d matches)\n", globMaxResults)
	}

	return Result{
		Content: b.String(),
		Meta: map[string]any{
			"pattern":   args.Pattern,
			"count":     len(matches),
			"truncated": truncated,
		},
	}, nil
}

// matchGlob matches slash-separated paths with * and **.
func matchGlob(pattern, name string) bool {
	return globMatch(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func globMatch(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		// ** matches zero or more path segments.
		if globMatch(pat[1:], name) {
			return true
		}
		if len(name) > 0 {
			return globMatch(pat, name[1:])
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if !matchSegment(pat[0], name[0]) {
		return false
	}
	return globMatch(pat[1:], name[1:])
}

// matchSegment matches one path segment using filepath.Match (* and ?).
func matchSegment(pattern, s string) bool {
	ok, err := filepath.Match(pattern, s)
	return err == nil && ok
}
