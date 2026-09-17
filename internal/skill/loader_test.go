package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
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

func TestDiscoverMissingDir(t *testing.T) {
	l := newLoader(t, t.TempDir())
	found, err := l.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 || l.CountActive() != 0 {
		t.Fatalf("found=%d active=%d", len(found), l.CountActive())
	}
}

func TestDiscoverAndActivate(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "go-testing", "# Go Testing\n\nAlways use table-driven tests.\n")
	writeSkill(t, root, "review", "# Code Review\n\nCheck error wrapping.\n")

	l := newLoader(t, root)
	found, err := l.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("found = %d", len(found))
	}
	names := []string{found[0].Name, found[1].Name}
	if names[0] != "go-testing" && names[1] != "go-testing" {
		t.Fatalf("names = %v", names)
	}
	if found[0].Summary == "" {
		t.Fatal("empty summary")
	}

	s, newly, err := l.Activate("go-testing")
	if err != nil {
		t.Fatal(err)
	}
	if !newly || s.Name != "go-testing" {
		t.Fatalf("activate: %+v newly=%v", s, newly)
	}
	if !l.IsActive("go-testing") {
		t.Fatal("not active")
	}

	// Second activate is not "new".
	_, newly, err = l.Activate("go-testing")
	if err != nil || newly {
		t.Fatalf("re-activate newly=%v err=%v", newly, err)
	}

	composed := l.Compose()
	if !strings.Contains(composed, "Skill: go-testing") || !strings.Contains(composed, "table-driven") {
		t.Fatalf("compose = %q", composed)
	}
	if strings.Contains(composed, "Code Review") {
		t.Fatalf("inactive skill leaked: %q", composed)
	}

	if !l.Deactivate("go-testing") {
		t.Fatal("deactivate failed")
	}
	if l.Compose() != "" {
		t.Fatalf("compose after deactivate = %q", l.Compose())
	}
}

func TestActivateUnknown(t *testing.T) {
	l := newLoader(t, t.TempDir())
	if _, _, err := l.Activate("nope"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestActivateWithoutDiscover(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "solo", "# Solo\nOnly this.\n")
	l := newLoader(t, root)
	s, newly, err := l.Activate("solo")
	if err != nil {
		t.Fatal(err)
	}
	if !newly || s.Name != "solo" {
		t.Fatalf("got %+v newly=%v", s, newly)
	}
}

func TestRejectPathTraversalName(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "ok", "ok")
	l := newLoader(t, root)
	if _, _, err := l.Activate("../ok"); err == nil {
		t.Fatal("expected invalid name")
	}
	if _, _, err := l.Activate(`..\ok`); err == nil {
		t.Fatal("expected invalid name")
	}
}

func TestDiscoverKeepsActivation(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "a", "# A\n")
	writeSkill(t, root, "b", "# B\n")
	l := newLoader(t, root)
	if _, err := l.Discover(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Activate("b"); err != nil {
		t.Fatal(err)
	}
	// Rediscover after adding another skill.
	writeSkill(t, root, "c", "# C\n")
	if _, err := l.Discover(); err != nil {
		t.Fatal(err)
	}
	if !l.IsActive("b") {
		t.Fatal("activation lost on rediscover")
	}
	if _, ok := l.Get("c"); !ok {
		t.Fatal("new skill not discovered")
	}
}

func TestEmptySkillIgnored(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "empty", "  \n")
	l := newLoader(t, root)
	found, err := l.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %+v", found)
	}
}

func TestExtractSummary(t *testing.T) {
	s := extractSummary("# Title\n\nbody line\n")
	if s != "Title" {
		t.Fatalf("heading summary = %q", s)
	}
	s = extractSummary("no heading\nmore\n")
	if s != "no heading" {
		t.Fatalf("fallback summary = %q", s)
	}
}
