// Package memory loads and updates the workspace MEMORY.md file.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileName is the cross-session memory document at the workspace root.
const FileName = "MEMORY.md"

// Store reads and appends to workspace/MEMORY.md.
type Store struct {
	workspace string
	path      string
	content   string
	loaded    bool
}

// New creates a memory store bound to a workspace root.
func New(workspace string) (*Store, error) {
	if workspace == "" {
		workspace = "."
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	abs = filepath.Clean(abs)
	return &Store{
		workspace: abs,
		path:      filepath.Join(abs, FileName),
	}, nil
}

// Path returns the absolute MEMORY.md path.
func (s *Store) Path() string { return s.path }

// RelPath is the workspace-relative path for display.
func (s *Store) RelPath() string { return FileName }

// Load reads MEMORY.md. Missing file is not an error (empty memory).
func (s *Store) Load() (string, bool, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.content = ""
			s.loaded = true
			return "", false, nil
		}
		return "", false, fmt.Errorf("read %s: %w", s.path, err)
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.TrimSpace(content)
	s.content = content
	s.loaded = true
	return content, true, nil
}

// Content returns the last loaded content (may be empty).
func (s *Store) Content() string {
	if !s.loaded {
		c, _, _ := s.Load()
		return c
	}
	return s.content
}

// Compose formats memory for context injection.
func (s *Store) Compose() string {
	c := strings.TrimSpace(s.Content())
	if c == "" {
		return ""
	}
	return "## Project memory (stable facts across sessions)\n\n" + c + "\n"
}

// Add appends one memory entry under a date heading and writes MEMORY.md.
func (s *Store) Add(entry string) (string, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", fmt.Errorf("empty memory entry")
	}
	entry = strings.ReplaceAll(entry, "\n", " ")
	line := "- " + entry

	existing := s.Content()
	var b strings.Builder
	stamp := time.Now().UTC().Format("2006-01-02")

	if existing == "" {
		b.WriteString("# Memory\n\n")
		b.WriteString("Stable project facts, constraints, and long-term decisions.\n\n")
		fmt.Fprintf(&b, "## %s\n\n", stamp)
		b.WriteString(line)
		b.WriteString("\n")
	} else {
		b.WriteString(existing)
		b.WriteString("\n")
		if strings.Contains(existing, "## "+stamp) {
			b.WriteString(line)
			b.WriteString("\n")
		} else {
			fmt.Fprintf(&b, "\n## %s\n\n%s\n", stamp, line)
		}
	}

	out := b.String()
	if err := os.WriteFile(s.path, []byte(out), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", s.path, err)
	}
	s.content = strings.TrimSpace(out)
	s.loaded = true
	return s.content, nil
}

// Exists reports whether MEMORY.md is present on disk.
func (s *Store) Exists() bool {
	_, err := os.Stat(s.path)
	return err == nil
}
