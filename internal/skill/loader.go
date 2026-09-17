// Package skill discovers and activates on-demand SKILL.md task knowledge.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DirName is the workspace subdirectory that holds skills.
const DirName = "skills"

// FileName is the markdown file expected inside each skill directory.
const FileName = "SKILL.md"

// Skill is one discoverable skill package.
type Skill struct {
	Name    string `json:"name"`
	Path    string `json:"path"`     // absolute path to SKILL.md
	RelPath string `json:"rel_path"` // relative to workspace
	Content string `json:"content"`
	// Summary is the first non-empty, non-heading line (or first heading text).
	Summary string `json:"summary"`
}

// Loader discovers skills under <workspace>/skills/ and tracks activation.
type Loader struct {
	workspace string
	skillsDir string
	available map[string]*Skill
	order     []string // discovery order (sorted by name)
	active    []string // activation order
}

// NewLoader creates a skill loader for a workspace.
func NewLoader(workspace string) (*Loader, error) {
	if workspace == "" {
		workspace = "."
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	abs = filepath.Clean(abs)
	return &Loader{
		workspace: abs,
		skillsDir: filepath.Join(abs, DirName),
		available: make(map[string]*Skill),
	}, nil
}

// Workspace returns the absolute workspace root.
func (l *Loader) Workspace() string { return l.workspace }

// SkillsDir returns the absolute skills directory path.
func (l *Loader) SkillsDir() string { return l.skillsDir }

// Discover scans <workspace>/skills/*/SKILL.md and caches results.
// Missing skills directory is not an error. Returns discovered skills sorted by name.
func (l *Loader) Discover() ([]*Skill, error) {
	entries, err := os.ReadDir(l.skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skills dir: %w", err)
	}

	// Rebuild available map from disk (idempotent rediscovery).
	prevActive := append([]string(nil), l.active...)
	l.available = make(map[string]*Skill)
	l.order = nil
	l.active = nil

	var found []*Skill
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		s, err := l.loadOne(e.Name())
		if err != nil {
			// Skip unreadable skill dirs rather than failing discovery.
			continue
		}
		if s == nil {
			continue
		}
		found = append(found, s)
		l.available[s.Name] = s
		l.order = append(l.order, s.Name)
	}
	sort.Strings(l.order)

	// Restore activations that still exist on disk.
	for _, name := range prevActive {
		if _, ok := l.available[name]; ok {
			l.active = append(l.active, name)
		}
	}
	return found, nil
}

// Available returns discovered skills sorted by name.
func (l *Loader) Available() []*Skill {
	out := make([]*Skill, 0, len(l.order))
	for _, name := range l.order {
		if s, ok := l.available[name]; ok {
			out = append(out, s)
		}
	}
	return out
}

// Get returns a discovered skill by name.
func (l *Loader) Get(name string) (*Skill, bool) {
	s, ok := l.available[name]
	return s, ok
}

// Activate marks a skill as loaded into context. Discovers on demand if the
// skill was not yet scanned. Returns the skill and whether it was newly activated.
func (l *Loader) Activate(name string) (*Skill, bool, error) {
	name = sanitizeName(name)
	if name == "" {
		return nil, false, fmt.Errorf("skill name required")
	}
	s, ok := l.available[name]
	if !ok {
		loaded, err := l.loadOne(name)
		if err != nil {
			return nil, false, err
		}
		if loaded == nil {
			return nil, false, fmt.Errorf("skill %q not found under %s", name, DirName)
		}
		s = loaded
		l.available[name] = s
		l.order = append(l.order, name)
		sort.Strings(l.order)
	}
	for _, a := range l.active {
		if a == name {
			return s, false, nil // already active
		}
	}
	l.active = append(l.active, name)
	return s, true, nil
}

// Deactivate removes a skill from the active set. Returns false if it was not active.
func (l *Loader) Deactivate(name string) bool {
	name = sanitizeName(name)
	for i, a := range l.active {
		if a == name {
			l.active = append(l.active[:i], l.active[i+1:]...)
			return true
		}
	}
	return false
}

// Active returns currently activated skills in activation order.
func (l *Loader) Active() []*Skill {
	out := make([]*Skill, 0, len(l.active))
	for _, name := range l.active {
		if s, ok := l.available[name]; ok {
			out = append(out, s)
		}
	}
	return out
}

// IsActive reports whether name is activated.
func (l *Loader) IsActive(name string) bool {
	name = sanitizeName(name)
	for _, a := range l.active {
		if a == name {
			return true
		}
	}
	return false
}

// CountActive returns how many skills are activated.
func (l *Loader) CountActive() int { return len(l.active) }

// Compose concatenates activated skills for injection into context.
func (l *Loader) Compose() string {
	active := l.Active()
	if len(active) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("The following task skills are active. Follow them when relevant.\n")
	for _, s := range active {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("### Skill: %s\n\n", s.Name))
		if s.Summary != "" {
			b.WriteString(fmt.Sprintf("%s\n\n", s.Summary))
		}
		b.WriteString(strings.TrimSpace(s.Content))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func (l *Loader) loadOne(name string) (*Skill, error) {
	name = sanitizeName(name)
	if name == "" || name == "." || name == ".." {
		return nil, nil
	}
	// Reject path separators — skill names are single directory names.
	if strings.ContainsAny(name, `/\`) {
		return nil, fmt.Errorf("invalid skill name %q", name)
	}
	path := filepath.Join(l.skillsDir, name, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skill %s: %w", name, err)
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}
	rel := filepath.ToSlash(filepath.Join(DirName, name, FileName))
	return &Skill{
		Name:    name,
		Path:    path,
		RelPath: rel,
		Content: content,
		Summary: extractSummary(content),
	}, nil
}

func sanitizeName(name string) string {
	return strings.TrimSpace(name)
}

// extractSummary pulls a one-line description from the skill body:
// prefer an H1 heading text, else the first non-empty non-heading line.
func extractSummary(content string) string {
	lines := strings.Split(content, "\n")
	var fallback string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "#") {
			t = strings.TrimSpace(strings.TrimLeft(t, "#"))
			if t != "" {
				return t
			}
			continue
		}
		if fallback == "" {
			fallback = t
		}
		// Keep scanning a few lines for a heading; otherwise use fallback.
	}
	return fallback
}
