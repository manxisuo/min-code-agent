// Package instruction loads hierarchical AGENTS.md project instructions.
package instruction

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileName is the instruction file name discovered in each directory.
const FileName = "AGENTS.md"

// skipDirs are never descended into when scanning for instructions.
var skipDirs = map[string]bool{
	".git":         true,
	".mincode":     true,
	"node_modules": true,
	"vendor":       true,
	".idea":        true,
	".vscode":      true,
}

// File is one loaded AGENTS.md document.
type File struct {
	Path    string `json:"path"`     // absolute path
	RelPath string `json:"rel_path"` // path relative to workspace (slash-separated)
	Dir     string `json:"dir"`      // absolute directory containing the file
	RelDir  string `json:"rel_dir"`  // directory relative to workspace ("." for root)
	Content string `json:"content"`
}

// Loader discovers and caches AGENTS.md files under a workspace.
// Files are ordered root → nested so more specific instructions win attention.
type Loader struct {
	workspace string // absolute, cleaned
	files     map[string]*File
	order     []string // absolute dirs in load order
}

// NewLoader creates a loader bound to an absolute workspace root.
func NewLoader(workspace string) (*Loader, error) {
	if workspace == "" {
		workspace = "."
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	return &Loader{
		workspace: filepath.Clean(abs),
		files:     make(map[string]*File),
	}, nil
}

// Workspace returns the absolute workspace root.
func (l *Loader) Workspace() string { return l.workspace }

// LoadRoot loads workspace/AGENTS.md if present.
// Returns (nil, nil) when the file does not exist.
func (l *Loader) LoadRoot() (*File, error) {
	return l.loadDir(l.workspace)
}

// LoadForPath ensures instructions for every directory between the workspace
// root and the directory containing path are loaded. Intermediate and target
// directories are included so nested rules apply when the agent touches files.
// path may be relative to the workspace or absolute (already inside it).
// Returns newly loaded files (may be empty).
func (l *Loader) LoadForPath(path string) ([]*File, error) {
	dir, err := l.dirOf(path)
	if err != nil {
		return nil, err
	}
	return l.loadChain(dir)
}

// LoadForDir is LoadForPath for a directory itself.
func (l *Loader) LoadForDir(dir string) ([]*File, error) {
	abs, err := l.resolveInside(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	return l.loadChain(abs)
}

// Files returns loaded instruction files in load order (root first).
func (l *Loader) Files() []*File {
	out := make([]*File, 0, len(l.order))
	for _, dir := range l.order {
		if f, ok := l.files[dir]; ok {
			out = append(out, f)
		}
	}
	return out
}

// Count returns how many instruction files are loaded.
func (l *Loader) Count() int { return len(l.order) }

// Has reports whether dir (absolute) already has a loaded instruction file.
func (l *Loader) Has(dir string) bool {
	_, ok := l.files[filepath.Clean(dir)]
	return ok
}

// Compose concatenates all loaded instructions. More specific (deeper)
// directories appear later so they can override general rules in practice.
func (l *Loader) Compose() string {
	files := l.Files()
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if f.RelDir == "." {
			b.WriteString(fmt.Sprintf("## Project instructions (%s)\n\n", f.RelPath))
		} else {
			b.WriteString(fmt.Sprintf("## Project instructions for %s/ (%s)\n\n", f.RelDir, f.RelPath))
		}
		b.WriteString(strings.TrimSpace(f.Content))
	}
	return b.String()
}

// Rel returns the slash-separated path of abs relative to the workspace.
func (l *Loader) Rel(abs string) string {
	rel, err := filepath.Rel(l.workspace, abs)
	if err != nil {
		return filepath.Base(abs)
	}
	return filepath.ToSlash(rel)
}

func (l *Loader) loadChain(dir string) ([]*File, error) {
	dir = filepath.Clean(dir)
	// Collect ancestors from root → dir so root loads first.
	var chain []string
	cur := dir
	for {
		chain = append(chain, cur)
		if strings.EqualFold(cur, l.workspace) {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Walked past the root without hitting it (path outside workspace).
			return nil, fmt.Errorf("path %q is outside workspace %q", dir, l.workspace)
		}
		cur = parent
	}
	// Reverse: root → target.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	var added []*File
	for _, d := range chain {
		if l.Has(d) {
			continue
		}
		f, err := l.loadDir(d)
		if err != nil {
			return added, err
		}
		if f != nil {
			added = append(added, f)
		}
	}
	return added, nil
}

func (l *Loader) loadDir(dir string) (*File, error) {
	dir = filepath.Clean(dir)
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}
	f := &File{
		Path:    path,
		RelPath: l.Rel(path),
		Dir:     dir,
		RelDir:  l.Rel(dir),
		Content: content,
	}
	if f.RelDir == "" {
		f.RelDir = "."
	}
	if _, exists := l.files[dir]; !exists {
		l.order = append(l.order, dir)
	}
	l.files[dir] = f
	return f, nil
}

func (l *Loader) dirOf(path string) (string, error) {
	abs, err := l.resolveInside(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		// Path may not exist yet (write_file target); use its parent dir.
		if os.IsNotExist(err) {
			return filepath.Dir(abs), nil
		}
		return "", err
	}
	if info.IsDir() {
		return abs, nil
	}
	return filepath.Dir(abs), nil
}

func (l *Loader) resolveInside(path string) (string, error) {
	if path == "" {
		path = "."
	}
	var abs string
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) {
		abs = filepath.Clean(path)
		if a, err := filepath.Abs(abs); err == nil {
			abs = a
		}
	} else {
		abs = filepath.Clean(filepath.Join(l.workspace, path))
	}
	// Soft boundary check: must stay under workspace.
	if !underWorkspace(l.workspace, abs) {
		return "", fmt.Errorf("path %q escapes workspace", path)
	}
	return abs, nil
}

func underWorkspace(root, abs string) bool {
	root = filepath.Clean(root)
	abs = filepath.Clean(abs)
	if strings.EqualFold(abs, root) {
		return true
	}
	prefix := root + string(filepath.Separator)
	return len(abs) > len(root) && strings.EqualFold(abs[:len(prefix)], prefix)
}

// ScanAll walks the workspace (skipping vendor dirs) and loads every AGENTS.md.
// Useful for offline inspection; the agent itself loads lazily via LoadForPath.
func (l *Loader) ScanAll() ([]*File, error) {
	var added []*File
	err := filepath.WalkDir(l.workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != l.workspace {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(d.Name(), FileName) {
			return nil
		}
		dir := filepath.Dir(path)
		if l.Has(dir) {
			return nil
		}
		f, ferr := l.loadDir(dir)
		if ferr != nil {
			return nil
		}
		if f != nil {
			added = append(added, f)
		}
		return nil
	})
	if err != nil {
		return added, err
	}
	// Stable order: shallower first, then path.
	sort.SliceStable(l.order, func(i, j int) bool {
		di, dj := depth(l.order[i], l.workspace), depth(l.order[j], l.workspace)
		if di != dj {
			return di < dj
		}
		return l.order[i] < l.order[j]
	})
	return added, nil
}

func depth(path, root string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0
	}
	if rel == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(rel), "/") + 1
}
