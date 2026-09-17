package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content, existed, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if existed || content != "" {
		t.Fatalf("content=%q existed=%v", content, existed)
	}
	if s.Compose() != "" {
		t.Fatal("compose should be empty")
	}
	if s.Exists() {
		t.Fatal("should not exist")
	}
}

func TestAddAndReload(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("Backend uses Go 1.22; do not bump without tests."); err != nil {
		t.Fatal(err)
	}
	if !s.Exists() {
		t.Fatal("memory.md missing")
	}
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Go 1.22") {
		t.Fatalf("file = %s", data)
	}

	// Second add same day appends bullet.
	if _, err := s.Add("CLI entry is cmd/mincode/main.go"); err != nil {
		t.Fatal(err)
	}
	c := s.Content()
	if !strings.Contains(c, "Go 1.22") || !strings.Contains(c, "cmd/mincode") {
		t.Fatalf("content = %q", c)
	}

	// New store loads from disk.
	s2, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, existed, err := s2.Load(); err != nil || !existed {
		t.Fatalf("reload existed=%v err=%v", existed, err)
	}
	if !strings.Contains(s2.Compose(), "Project memory") {
		t.Fatalf("compose = %q", s2.Compose())
	}
}

func TestAddEmpty(t *testing.T) {
	s, _ := New(t.TempDir())
	if _, err := s.Add("  "); err == nil {
		t.Fatal("expected error")
	}
}
