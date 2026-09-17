package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manxisuo/mincode/internal/llm"
	"github.com/manxisuo/mincode/internal/observability"
	"github.com/manxisuo/mincode/internal/plan"
)

func newPlanApp(t *testing.T) *App {
	t.Helper()
	wsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
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
	return app
}

func TestPlanDraftApproveReject(t *testing.T) {
	app := newPlanApp(t)
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{Content: "1. Read main.go\n2. Summarize package\n3. Done check\n"},
		},
	}
	app.agent.Provider = fake
	app.provider = fake

	var buf bytes.Buffer
	app.out = &buf
	ctx := context.Background()

	app.handleCommand(ctx, "/plan analyze main package")
	out := buf.String()
	if !strings.Contains(out, "Read main.go") {
		t.Fatalf("draft = %q", out)
	}
	p := app.plans.Current()
	if p == nil || p.Status != plan.StatusDraft || len(p.Steps) != 3 {
		t.Fatalf("plan = %+v", p)
	}

	// Events: plan.created
	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}
	created := false
	for _, e := range events {
		if e.Type == observability.EventPlanCreated {
			created = true
		}
	}
	if !created {
		t.Fatal("missing plan.created")
	}

	// Reject works.
	buf.Reset()
	app.handleCommand(ctx, "/plan reject")
	if !strings.Contains(buf.String(), "rejected") {
		t.Fatalf("reject = %q", buf.String())
	}
	if app.plans.Current() != nil {
		t.Fatal("plan should be cleared after reject")
	}
}

func TestPlanExecuteSteps(t *testing.T) {
	app := newPlanApp(t)

	// Provider: first call = plan draft; subsequent = step answers.
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{Content: "1. Read main.go\n2. Report package name\n"},
			{Content: "Read package main"},
			{Content: "package is main"},
		},
	}
	app.agent.Provider = fake
	app.provider = fake

	var buf bytes.Buffer
	app.out = &buf
	ctx := context.Background()

	app.handleCommand(ctx, "/plan inspect package")
	if app.plans.Current() == nil {
		t.Fatal("no plan drafted")
	}

	buf.Reset()
	app.handleCommand(ctx, "/plan approve")
	out := buf.String()
	if !strings.Contains(out, "step 1/2") || !strings.Contains(out, "step 2/2") {
		t.Fatalf("execute = %q", out)
	}
	if !strings.Contains(out, "complete") {
		t.Fatalf("expected completion: %q", out)
	}

	p := app.plans.Current()
	if p.Status != plan.StatusDone {
		t.Fatalf("plan status = %s", p.Status)
	}
	if p.Steps[0].Status != plan.StatusDone || p.Steps[1].Status != plan.StatusDone {
		t.Fatalf("steps = %+v", p.Steps)
	}

	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}
	want := map[observability.EventType]bool{
		observability.EventPlanCreated:      false,
		observability.EventPlanApproved:     false,
		observability.EventPlanStepStarted:  false,
		observability.EventPlanStepFinished: false,
		observability.EventPlanFinished:     false,
	}
	for _, e := range events {
		if _, ok := want[e.Type]; ok {
			want[e.Type] = true
		}
	}
	for typ, ok := range want {
		if !ok {
			t.Fatalf("missing event %s", typ)
		}
	}

	buf.Reset()
	app.handleCommand(ctx, "/timeline")
	if !strings.Contains(buf.String(), "Plan Approved") {
		t.Fatalf("timeline = %q", buf.String())
	}
}

func TestPlanApproveWithoutDraft(t *testing.T) {
	app := newPlanApp(t)
	var buf bytes.Buffer
	app.out = &buf
	app.handleCommand(context.Background(), "/plan approve")
	if !strings.Contains(buf.String(), "no active plan") {
		t.Fatalf("out = %q", buf.String())
	}
}

func TestPlanShowEmpty(t *testing.T) {
	app := newPlanApp(t)
	var buf bytes.Buffer
	app.out = &buf
	app.handleCommand(context.Background(), "/plan")
	if !strings.Contains(buf.String(), "no active plan") {
		t.Fatalf("out = %q", buf.String())
	}
}
