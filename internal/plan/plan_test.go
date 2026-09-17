package plan

import (
	"strings"
	"testing"
)

func TestNewPlanAndApprove(t *testing.T) {
	p := NewPlan("p1", "add health endpoint", []string{
		"read router",
		"add /health",
		"write tests",
		"",
	})
	if len(p.Steps) != 3 {
		t.Fatalf("steps = %d", len(p.Steps))
	}
	if p.Status != StatusDraft {
		t.Fatalf("status = %s", p.Status)
	}
	if p.Steps[0].Index != 1 || p.Steps[2].Index != 3 {
		t.Fatalf("indexes = %+v", p.Steps)
	}
	if err := p.Approve(); err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusApproved {
		t.Fatalf("status = %s", p.Status)
	}
	// Cannot approve twice.
	if err := p.Approve(); err == nil {
		t.Fatal("expected re-approve error")
	}
}

func TestApproveEmpty(t *testing.T) {
	p := NewPlan("p", "goal", nil)
	if err := p.Approve(); err == nil {
		t.Fatal("expected error for empty plan")
	}
}

func TestReject(t *testing.T) {
	p := NewPlan("p", "g", []string{"a"})
	if err := p.Reject(); err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusRejected {
		t.Fatalf("status = %s", p.Status)
	}
}

func TestStepLifecycle(t *testing.T) {
	p := NewPlan("p", "g", []string{"a", "b"})
	if err := p.Approve(); err != nil {
		t.Fatal(err)
	}
	if err := p.StartStep(1); err != nil {
		t.Fatal(err)
	}
	if p.Steps[0].Status != StatusRunning || p.Current != 1 {
		t.Fatalf("step0 = %+v current=%d", p.Steps[0], p.Current)
	}
	if err := p.CompleteStep(1, "read done"); err != nil {
		t.Fatal(err)
	}
	if err := p.StartStep(2); err != nil {
		t.Fatal(err)
	}
	if err := p.FailStep(2, "compile error"); err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusFailed {
		t.Fatalf("status = %s", p.Status)
	}
	done, total := p.Progress()
	if done != 1 || total != 2 {
		t.Fatalf("progress = %d/%d", done, total)
	}
}

func TestFinishAllDone(t *testing.T) {
	p := NewPlan("p", "g", []string{"a", "b"})
	_ = p.Approve()
	_ = p.StartStep(1)
	_ = p.CompleteStep(1, "")
	_ = p.StartStep(2)
	_ = p.CompleteStep(2, "")
	p.Finish()
	if p.Status != StatusDone {
		t.Fatalf("status = %s", p.Status)
	}
}

func TestCancel(t *testing.T) {
	p := NewPlan("p", "g", []string{"a"})
	_ = p.Approve()
	p.Cancel()
	if p.Status != StatusCancelled {
		t.Fatalf("status = %s", p.Status)
	}
}

func TestParseStepList(t *testing.T) {
	text := `1. Read the router file
2) Add /health handler
- Write unit tests
* Run go test
random prose line should be ignored
4、Update docs
`
	got := ParseStepList(text)
	want := []string{
		"Read the router file",
		"Add /health handler",
		"Write unit tests",
		"Run go test",
		"Update docs",
	}
	if len(got) != len(want) {
		t.Fatalf("got = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCancelStepMarksPlanCancelled(t *testing.T) {
	p := NewPlan("p", "g", []string{"a", "b"})
	_ = p.Approve()
	_ = p.StartStep(1)
	if err := p.CancelStep(1, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusCancelled {
		t.Fatalf("plan status = %s, want cancelled", p.Status)
	}
	if p.Steps[0].Status != StatusCancelled {
		t.Fatalf("step status = %s", p.Steps[0].Status)
	}
	if p.Steps[0].Error != "cancelled" {
		t.Fatalf("step error = %q", p.Steps[0].Error)
	}
	// Re-approve after cancel should fail (not draft).
	if err := p.Approve(); err == nil {
		t.Fatal("expected approve error on cancelled plan")
	}
}

func TestCancelOnlyApprovedOrRunning(t *testing.T) {
	p := NewPlan("p", "g", []string{"a"})
	// draft: cancel is no-op
	p.Cancel()
	if p.Status != StatusDraft {
		t.Fatalf("draft cancel changed status to %s", p.Status)
	}
	_ = p.Approve()
	p.Cancel()
	if p.Status != StatusCancelled {
		t.Fatalf("status = %s", p.Status)
	}
}

func TestFormat(t *testing.T) {
	p := NewPlan("p1", "do thing", []string{"step one"})
	_ = p.Approve()
	out := p.Format()
	if !strings.Contains(out, "do thing") || !strings.Contains(out, "step one") {
		t.Fatalf("format = %q", out)
	}
	if !strings.Contains(out, "approved") {
		t.Fatalf("format = %q", out)
	}
}
