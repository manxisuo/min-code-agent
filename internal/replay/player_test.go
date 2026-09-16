package replay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mincode/mincode/internal/observability"
)

func writeTrace(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	rec, err := observability.NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		e := observability.NewEvent("s", i, observability.EventAgentStateChanged, observability.StateChangedData{To: "X"})
		if err := rec.Record(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPlayerNavigation(t *testing.T) {
	path := writeTrace(t, 5)
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Len() != 5 {
		t.Fatalf("len = %d", p.Len())
	}
	if p.Index() != 0 {
		t.Fatal("should start at 0")
	}
	if !p.Next() || p.Index() != 1 {
		t.Fatal("next")
	}
	if !p.Next() || !p.Next() {
		t.Fatal("next more")
	}
	if !p.Prev() || p.Index() != 2 {
		t.Fatal("prev")
	}
	if !p.Goto(5) || p.Index() != 5 {
		t.Fatal("goto")
	}
	if p.Next() {
		t.Fatal("should be at end")
	}
	if !p.Goto(1) {
		t.Fatal("goto 1")
	}
	if p.Prev() {
		t.Fatal("already at start")
	}
	if p.Goto(0) || p.Goto(6) {
		t.Fatal("out of range")
	}
}

func TestPlayerSummary(t *testing.T) {
	path := writeTrace(t, 3)
	p, _ := Load(path)
	var b strings.Builder
	p.Summary(&b)
	if !strings.Contains(b.String(), "agent.state_changed") {
		t.Fatalf("summary = %s", b.String())
	}
}

func TestLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for empty trace")
	}
}

func TestPrintCurrent(t *testing.T) {
	path := writeTrace(t, 1)
	p, _ := Load(path)
	p.Next()
	var b strings.Builder
	p.PrintCurrent(&b)
	if !strings.Contains(b.String(), "Step 1 / 1") {
		t.Fatalf("out = %s", b.String())
	}
	_ = time.Now
}
