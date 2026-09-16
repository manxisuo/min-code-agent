package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	grepMaxMatches      = 100
	grepMaxFileBytes    = 1 << 20 // skip files larger than 1MB
	grepMaxOutputBytes  = 64 * 1024
	grepDefaultSkipDirs = ".git;.mincode;node_modules;vendor;__pycache__"
)

// Grep searches file contents with a regular expression.
type Grep struct {
	WS *Workspace
}

func (t *Grep) Name() string { return "grep" }

func (t *Grep) Description() string {
	return "Search file contents in the workspace using a regular expression. Optionally filter files with a glob file_pattern."
}

func (t *Grep) Schema() JSONSchema {
	return JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression to search for",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search (default: workspace root)",
			},
			"file_pattern": map[string]any{
				"type":        "string",
				"description": "Glob filter for filenames, e.g. *.go (default: all files)",
			},
		},
		"required": []string{"pattern"},
	}
}

type grepArgs struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path"`
	FilePattern string `json:"file_pattern"`
}

func (t *Grep) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var args grepArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if args.Pattern == "" {
		return Result{Content: "pattern is required", IsError: true}, nil
	}

	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return Result{Content: "invalid regexp: " + err.Error(), IsError: true}, nil
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
		// Allow grepping a single file.
		matches, err := grepFile(ctx, abs, abs, re)
		if err != nil {
			return Result{Content: err.Error(), IsError: true}, nil
		}
		return grepResult(args, matches, false)
	}

	skip := map[string]bool{}
	for _, d := range strings.Split(grepDefaultSkipDirs, ";") {
		skip[d] = true
	}

	var matches []grepMatch
	truncated := false
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if args.FilePattern != "" && !fileNameMatches(args.FilePattern, d.Name()) {
			rel, _ := filepath.Rel(abs, path)
			if !fileNameMatches(args.FilePattern, filepath.ToSlash(rel)) {
				return nil
			}
		}
		fi, err := d.Info()
		if err != nil || fi.Size() > grepMaxFileBytes {
			return nil
		}
		ms, err := grepFile(ctx, abs, path, re)
		if err != nil {
			return nil
		}
		matches = append(matches, ms...)
		if len(matches) >= grepMaxMatches {
			truncated = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && err != fs.SkipAll {
		return Result{Content: fmt.Sprintf("walk: %v", err), IsError: true}, nil
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Path != matches[j].Path {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].Line < matches[j].Line
	})
	if len(matches) > grepMaxMatches {
		matches = matches[:grepMaxMatches]
		truncated = true
	}
	return grepResult(args, matches, truncated)
}

type grepMatch struct {
	Path string
	Line int
	Text string
}

func grepFile(ctx context.Context, root, path string, re *regexp.Regexp) ([]grepMatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Binary sniff.
	var sniff [512]byte
	n, _ := f.Read(sniff[:])
	if isBinary(sniff[:n]) {
		return nil, nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	var out []grepMatch
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
	lineNo := 0
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineNo++
		line := sc.Text()
		if re.MatchString(line) {
			text := line
			if len(text) > 200 {
				text = text[:200] + "..."
			}
			out = append(out, grepMatch{Path: rel, Line: lineNo, Text: text})
			if len(out) >= grepMaxMatches {
				break
			}
		}
	}
	return out, sc.Err()
}

func grepResult(args grepArgs, matches []grepMatch, truncated bool) (Result, error) {
	if len(matches) == 0 {
		return Result{
			Content: "no matches",
			Meta: map[string]any{
				"pattern": args.Pattern,
				"count":   0,
			},
		}, nil
	}

	var b strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&b, "%s:%d: %s\n", m.Path, m.Line, m.Text)
	}
	if truncated {
		fmt.Fprintf(&b, "... (truncated at %d matches)\n", grepMaxMatches)
	}
	content := b.String()
	if len(content) > grepMaxOutputBytes {
		content = content[:grepMaxOutputBytes] + "\n... (output truncated)\n"
	}

	return Result{
		Content: content,
		Meta: map[string]any{
			"pattern":   args.Pattern,
			"count":     len(matches),
			"truncated": truncated,
		},
	}, nil
}

// fileNameMatches reports whether name matches a simple glob pattern (*, ?).
func fileNameMatches(pattern, name string) bool {
	ok, err := filepath.Match(pattern, filepath.Base(name))
	if err != nil {
		return false
	}
	if ok {
		return true
	}
	// Also try matching the full slash path (e.g. **/*.go → *.go on base already covered).
	ok, err = filepath.Match(pattern, name)
	return err == nil && ok
}
