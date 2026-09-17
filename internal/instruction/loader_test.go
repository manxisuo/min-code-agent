package instruction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newLoader(t *testing.T, root string) *Loader {
	t.Helper()
	l, err := NewLoader(root)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestLoadRootMissing(t *testing.T) {
	root := t.TempDir()
	l := newLoader(t, root)
	f, err := l.LoadRoot()
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatalf("expected nil file, got %+v", f)
	}
	if l.Count() != 0 {
		t.Fatalf("count = %d", l.Count())
	}
	if l.Compose() != "" {
		t.Fatalf("compose = %q", l.Compose())
	}
}

func TestLoadRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Rules\nAlways run tests.\n")
	l := newLoader(t, root)

	f, err := l.LoadRoot()
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatal("expected root file")
	}
	if f.RelDir != "." {
		t.Fatalf("reldir = %q", f.RelDir)
	}
	if f.RelPath != "AGENTS.md" {
		t.Fatalf("relpath = %q", f.RelPath)
	}
	if !strings.Contains(f.Content, "Always run tests") {
		t.Fatalf("content = %q", f.Content)
	}
	if l.Count() != 1 {
		t.Fatalf("count = %d", l.Count())
	}

	// Second load is a no-op (already cached).
	f2, err := l.LoadRoot()
	if err != nil || f2 == nil {
		t.Fatalf("reload: %v %v", f2, err)
	}
	if l.Count() != 1 {
		t.Fatalf("count after reload = %d", l.Count())
	}
}

func TestLoadForPathHierarchy(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "root rules")
	writeFile(t, filepath.Join(root, "backend", "AGENTS.md"), "backend rules")
	writeFile(t, filepath.Join(root, "backend", "api", "handlers.go"), "package api\n")
	// No AGENTS.md in backend/api — should still load ancestors.

	l := newLoader(t, root)
	if _, err := l.LoadRoot(); err != nil {
		t.Fatal(err)
	}
	added, err := l.LoadForPath("backend/api/handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 {
		t.Fatalf("added = %d (%+v)", len(added), added)
	}
	if added[0].RelDir != "backend" {
		t.Fatalf("added dir = %q", added[0].RelDir)
	}
	files := l.Files()
	if len(files) != 2 {
		t.Fatalf("files = %d", len(files))
	}
	if files[0].RelDir != "." || files[1].RelDir != "backend" {
		t.Fatalf("order = %s, %s", files[0].RelDir, files[1].RelDir)
	}

	composed := l.Compose()
	if !strings.Contains(composed, "root rules") || !strings.Contains(composed, "backend rules") {
		t.Fatalf("compose = %q", composed)
	}
	// Specific section comes after general.
	if strings.Index(composed, "root rules") > strings.Index(composed, "backend rules") {
		t.Fatalf("expected root before backend:\n%s", composed)
	}
}

func TestLoadForPathOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	l := newLoader(t, root)
	if _, err := l.LoadForPath("../outside.txt"); err == nil {
		t.Fatal("expected escape error")
	}
}

func TestLoadEmptyFileIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "   \n\n")
	l := newLoader(t, root)
	f, err := l.LoadRoot()
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatalf("empty file should be ignored, got %+v", f)
	}
}

func TestLoadForPathAbsolute(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "AGENTS.md"), "pkg rules")
	writeFile(t, filepath.Join(root, "pkg", "a.go"), "package pkg\n")
	abs := filepath.Join(root, "pkg", "a.go")
	l := newLoader(t, root)
	added, err := l.LoadForPath(abs)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0].RelDir != "pkg" {
		t.Fatalf("added = %+v", added)
	}
}

func TestScanAll(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "root")
	writeFile(t, filepath.Join(root, "svc", "AGENTS.md"), "svc")
	writeFile(t, filepath.Join(root, "vendor", "AGENTS.md"), "vendor-should-skip")
	writeFile(t, filepath.Join(root, "node_modules", "x", "AGENTS.md"), "nm-should-skip")

	l := newLoader(t, root)
	added, err := l.ScanAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 {
		t.Fatalf("added = %d (%+v)", len(added), added)
	}
	files := l.Files()
	if files[0].RelDir != "." || files[1].RelDir != "svc" {
		t.Fatalf("order = %v", files)
	}
	for _, f := range files {
		if strings.Contains(f.RelDir, "vendor") || strings.Contains(f.RelDir, "node_modules") {
			t.Fatalf("skipped dir leaked: %s", f.RelDir)
		}
	}
}

func TestLoadForDir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "AGENTS.md"), "docs rules")
	l := newLoader(t, root)
	added, err := l.LoadForDir("docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0].RelDir != "docs" {
		t.Fatalf("added = %+v", added)
	}
}

func TestCRLFNormalized(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "line1\r\nline2\r\n")
	l := newLoader(t, root)
	f, err := l.LoadRoot()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.Content, "\r") {
		t.Fatalf("content still has CR: %q", f.Content)
	}
}
