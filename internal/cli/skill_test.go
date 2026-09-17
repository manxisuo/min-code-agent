package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mincode/mincode/internal/llm"
	"github.com/mincode/mincode/internal/observability"
)

func newAppWithSkills(t *testing.T) (*App, string) {
	t.Helper()
	wsDir := t.TempDir()
	skillDir := filepath.Join(wsDir, "skills", "go-testing")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# Go Testing\n\nAlways use table-driven tests.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	traceDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "mincode.yaml")
	yaml := "provider:\n  type: fake\n  model: fake-model\ntrace:\n  dir: " + filepath.ToSlash(traceDir) + "\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	app, err := NewApp(Options{ConfigPath: cfgPath, Workspace: wsDir})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	app.out = &buf
	t.Cleanup(func() { _ = app.Close() })
	return app, wsDir
}

func TestSkillsDiscoveredAtStartup(t *testing.T) {
	app, _ := newAppWithSkills(t)
	if len(app.skills.Available()) != 1 {
		t.Fatalf("available = %d", len(app.skills.Available()))
	}
	if app.skills.CountActive() != 0 {
		t.Fatal("skills should start inactive")
	}

	var buf bytes.Buffer
	app.out = &buf
	app.handleCommand("/skills")
	out := buf.String()
	if !strings.Contains(out, "go-testing") || !strings.Contains(out, "inactive") {
		t.Fatalf("skills list = %q", out)
	}
}

func TestSkillActivateDeactivate(t *testing.T) {
	app, _ := newAppWithSkills(t)
	var buf bytes.Buffer
	app.out = &buf

	app.handleCommand("/skill go-testing")
	out := buf.String()
	if !strings.Contains(out, "activated") {
		t.Fatalf("activate = %q", out)
	}
	if !app.skills.IsActive("go-testing") {
		t.Fatal("not active")
	}
	if !strings.Contains(app.agent.Ctx.Skills(), "table-driven") {
		t.Fatalf("ctx skills = %q", app.agent.Ctx.Skills())
	}

	buf.Reset()
	app.handleCommand("/skill go-testing")
	if !strings.Contains(buf.String(), "already active") {
		t.Fatalf("re-activate = %q", buf.String())
	}

	buf.Reset()
	app.handleCommand("/skill -go-testing")
	if !strings.Contains(buf.String(), "deactivated") {
		t.Fatalf("deactivate = %q", buf.String())
	}
	if app.agent.Ctx.Skills() != "" {
		t.Fatalf("ctx skills after deactivate = %q", app.agent.Ctx.Skills())
	}

	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}
	var loaded, unloaded bool
	for _, e := range events {
		if e.Type == observability.EventSkillLoaded {
			loaded = true
		}
		if e.Type == observability.EventSkillUnloaded {
			unloaded = true
		}
	}
	if !loaded || !unloaded {
		t.Fatalf("events loaded=%v unloaded=%v", loaded, unloaded)
	}
}

func TestSkillUnknownName(t *testing.T) {
	app, _ := newAppWithSkills(t)
	var buf bytes.Buffer
	app.out = &buf
	app.handleCommand("/skill does-not-exist")
	if !strings.Contains(buf.String(), "not found") {
		t.Fatalf("unknown skill = %q", buf.String())
	}
}

func TestSkillsEnterContextSnapshot(t *testing.T) {
	app, _ := newAppWithSkills(t)
	var buf bytes.Buffer
	app.out = &buf
	app.handleCommand("/skill go-testing")

	fake := &llm.FakeProvider{Responses: []llm.ChatResponse{{Content: "ok"}}}
	app.agent.Provider = fake
	if err := app.runTurn(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}

	reqs := fake.Requests()
	if len(reqs) == 0 {
		t.Fatal("no LLM requests")
	}
	found := false
	for _, m := range reqs[0].Messages {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "table-driven") {
			found = true
		}
	}
	if !found {
		t.Fatalf("skill content not in system messages: %+v", reqs[0].Messages)
	}

	buf.Reset()
	app.handleCommand("/context")
	if !strings.Contains(buf.String(), "skills") {
		t.Fatalf("context snapshot missing skills source: %q", buf.String())
	}

	buf.Reset()
	app.handleCommand("/timeline")
	if !strings.Contains(buf.String(), "Skill Loaded") {
		t.Fatalf("timeline missing skill: %q", buf.String())
	}
}
