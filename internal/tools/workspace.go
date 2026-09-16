package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Workspace is the security boundary for all file-oriented tools.
type Workspace struct {
	root string // absolute, cleaned
}

// NewWorkspace resolves root to an absolute path.
func NewWorkspace(root string) (*Workspace, error) {
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace %s is not a directory", abs)
	}
	return &Workspace{root: abs}, nil
}

// Root returns the absolute workspace root.
func (w *Workspace) Root() string { return w.root }

// Resolve maps a model-supplied path to an absolute path inside the workspace.
// It rejects path traversal, absolute escapes, and symlink escapes.
func (w *Workspace) Resolve(p string) (string, error) {
	if p == "" {
		p = "."
	}
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("invalid path")
	}

	// Treat POSIX-style absolute paths as absolute on all platforms so
	// "/etc/passwd" cannot be silently joined into the workspace on Windows.
	isAbs := filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`)

	var candidate string
	if isAbs {
		candidate = filepath.Clean(p)
		if abs, err := filepath.Abs(candidate); err == nil {
			candidate = abs
		}
	} else {
		candidate = filepath.Clean(filepath.Join(w.root, p))
	}

	real, err := evalDeepest(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", p, err)
	}

	if !w.contains(real) {
		return "", fmt.Errorf("path %q escapes workspace", p)
	}
	return real, nil
}

func (w *Workspace) contains(abs string) bool {
	root := w.root
	// Case-insensitive compare on Windows.
	if strings.EqualFold(abs, root) {
		return true
	}
	prefix := root + string(filepath.Separator)
	return len(abs) > len(root) && strings.EqualFold(abs[:len(prefix)], prefix)
}

// evalDeepest evaluates symlinks on the longest existing ancestor, then
// re-appends the non-existing tail.
func evalDeepest(abs string) (string, error) {
	current := abs
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if len(tail) == 0 {
				return filepath.Clean(resolved), nil
			}
			// Re-join unresolved tail (non-existent components).
			parts := make([]string, 0, len(tail)+1)
			parts = append(parts, resolved)
			for i := len(tail) - 1; i >= 0; i-- {
				parts = append(parts, tail[i])
			}
			return filepath.Clean(filepath.Join(parts...)), nil
		}
		if !os.IsNotExist(err) {
			// Broken permission or other FS error: fall back to lexical path.
			return abs, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs, nil
		}
		tail = append(tail, filepath.Base(current))
		current = parent
	}
}
