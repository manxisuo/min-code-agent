package paths

import (
	"strings"
	"testing"
)

func TestProjectIDCollisionAvoidance(t *testing.T) {
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
	if strings.Contains(l.TracesDir, "/tmp/demo/") {
		t.Fatalf("global traces must not live under workspace: %s", l.TracesDir)
	}
}

func TestResolveWorkspace(t *testing.T) {
	l := Resolve(`/tmp/demo`, LocationWorkspace, "")
	if !strings.HasSuffix(l.TracesDir, ".mincode") && !strings.Contains(l.TracesDir, ".mincode") {
		t.Fatalf("traces = %s", l.TracesDir)
	}
}

func TestSlugNameSanitized(t *testing.T) {
	s := SlugName(`D:\Weird:Name*`)
	if strings.ContainsAny(s, `<>:"/\|?*`) {
		t.Fatalf("slug = %q", s)
	}
}
