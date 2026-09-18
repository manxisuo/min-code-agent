package paths

import (
	"strings"
	"testing"
)

func TestProjectIDCollisionAvoidance(t *testing.T) {
	// Classic dash-collision case.
	a := ProjectID(`D:\A-B\C`)
	b := ProjectID(`D:\A\B-C`)
	if a == b {
		t.Fatalf("project id collision: %s vs %s", a, b)
	}
	if !strings.HasPrefix(a, "C-") || !strings.HasPrefix(b, "B-C-") {
		t.Fatalf("slug parts: %s | %s", a, b)
	}
}

func TestProjectIDStable(t *testing.T) {
	p := `D:\Code\min-code-agent`
	if ProjectID(p) != ProjectID(p) {
		t.Fatal("id not stable")
	}
	if !strings.HasPrefix(ProjectID(p), "min-code-agent-") {
		t.Fatalf("id = %s", ProjectID(p))
	}
}

func TestResolveGlobal(t *testing.T) {
	l := Resolve(`/tmp/demo`, LocationGlobal, `/tmp/mc-data`)
	if !strings.Contains(l.ProjectDir, "demo-") {
		t.Fatalf("project dir = %s", l.ProjectDir)
	}
	if !strings.HasPrefix(l.TracesDir, l.ProjectDir) {
		t.Fatalf("traces = %s", l.TracesDir)
	}
	if l.LegacyTraces == "" || !strings.Contains(l.LegacyTraces, ".mincode") {
		t.Fatalf("legacy traces = %s", l.LegacyTraces)
	}
	if len(l.TraceSearchDirs()) < 2 {
		t.Fatalf("search dirs = %v", l.TraceSearchDirs())
	}
}

func TestResolveWorkspace(t *testing.T) {
	l := Resolve(`/tmp/demo`, LocationWorkspace, "")
	if !strings.HasSuffix(l.TracesDir, `traces`) {
		t.Fatalf("traces = %s", l.TracesDir)
	}
	if len(l.TraceSearchDirs()) != 1 {
		t.Fatalf("workspace mode should not duplicate legacy: %v", l.TraceSearchDirs())
	}
}

func TestSlugNameSanitized(t *testing.T) {
	s := SlugName(`D:\Weird:Name*`)
	if strings.ContainsAny(s, `<>:"/\|?*`) {
		t.Fatalf("slug = %q", s)
	}
}
