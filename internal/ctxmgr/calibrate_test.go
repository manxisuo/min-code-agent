package ctxmgr

import "testing"

func TestCalibratorObserveAndScale(t *testing.T) {
	c := NewCalibrator()
	if c.Ratio() != 1.0 {
		t.Fatalf("initial ratio = %v", c.Ratio())
	}
	if c.Scale(100) != 100 {
		t.Fatalf("scale at 1.0 = %d", c.Scale(100))
	}

	// Actual is 2x local estimate → ratio should rise above 1.
	c.Observe(1000, 2000)
	if c.Ratio() <= 1.0 {
		t.Fatalf("ratio after under-estimate = %v", c.Ratio())
	}
	if c.Scale(1000) < 1500 {
		t.Fatalf("scaled = %d", c.Scale(1000))
	}

	est, act := c.Last()
	if est != 1000 || act != 2000 {
		t.Fatalf("last = %d,%d", est, act)
	}
}

func TestCalibratorClamps(t *testing.T) {
	c := NewCalibrator()
	for i := 0; i < 20; i++ {
		c.Observe(1000, 100000) // wildly high
	}
	if c.Ratio() > 2.5 {
		t.Fatalf("ratio not clamped: %v", c.Ratio())
	}
	for i := 0; i < 40; i++ {
		c.Observe(100000, 100) // wildly low
	}
	if c.Ratio() < 0.5 {
		t.Fatalf("ratio not clamped low: %v", c.Ratio())
	}
}

func TestObserveUsageUpdatesSnapshot(t *testing.T) {
	m := New("SYS", "", 10000)
	m.AppendUser("hello")
	_, snap := m.BuildRequest(nil)
	est := snap.TotalTokens + snap.ToolTokens

	m.ObserveUsage(est, est*2)
	got := m.LastSnapshot()
	if got == nil || got.ActualPromptTokens != est*2 {
		t.Fatalf("snapshot actual = %+v", got)
	}
	if got.EstimateRatio <= 1.0 {
		t.Fatalf("ratio = %v", got.EstimateRatio)
	}

	// Subsequent estimates should be scaled up.
	m.AppendUser("again")
	_, snap2 := m.BuildRequest(nil)
	base := EstimateTokens("again")
	if snap2.Items == nil {
		t.Fatal("no items")
	}
	// user_input item should be >= base (ratio > 1)
	found := false
	for _, it := range snap2.Items {
		if it.Source == SourceUserInput {
			found = true
			if it.Tokens < base {
				t.Fatalf("expected scaled tokens >= %d, got %d", base, it.Tokens)
			}
		}
	}
	if !found {
		t.Fatal("missing user_input")
	}
}

func TestManagerObserveIgnoresInvalid(t *testing.T) {
	m := New("S", "", 100)
	m.ObserveUsage(0, 0)
	if m.Calibrator().Samples() != 0 {
		t.Fatal("should ignore zero samples")
	}
}
