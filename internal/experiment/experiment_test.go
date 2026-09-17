package experiment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreSaveLoadSummarize(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r1 := RunResult{
		RunID: "r1", Experiment: "base", Task: "analyze", Model: "m1",
		Success: true, Steps: 4, ToolCalls: 3, LLMCalls: 5,
		InputTokens: 100, OutputTokens: 50, TotalTokens: 150,
		DurationMS: 1000, FinishedAt: time.Now().UTC(),
	}
	r2 := RunResult{
		RunID: "r2", Experiment: "base", Task: "analyze", Model: "m1",
		Success: false, Steps: 2, ToolCalls: 1, LLMCalls: 2,
		InputTokens: 80, OutputTokens: 20, TotalTokens: 100,
		DurationMS: 500, Failures: 1,
	}
	if err := st.Save(r1); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(r2); err != nil {
		t.Fatal(err)
	}

	runs, err := st.LoadExperiment("base")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d", len(runs))
	}

	agg := Summarize("base", runs)
	if agg.Runs != 2 || agg.Successes != 1 {
		t.Fatalf("agg = %+v", agg)
	}
	if agg.AvgTotalTok != 125 {
		t.Fatalf("avg total = %v", agg.AvgTotalTok)
	}
	if agg.TotalFailures != 1 {
		t.Fatalf("failures = %d", agg.TotalFailures)
	}

	names, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "base" {
		t.Fatalf("names = %v", names)
	}

	// File layout
	if _, err := os.Stat(filepath.Join(st.Root(), "base", "r1.json")); err != nil {
		t.Fatal(err)
	}
}

func TestFormatReports(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Save(RunResult{RunID: "a1", Experiment: "A", Model: "m", Success: true, Steps: 3, TotalTokens: 200, DurationMS: 10})
	_ = st.Save(RunResult{RunID: "b1", Experiment: "B", Model: "m2", Success: true, Steps: 5, TotalTokens: 400, DurationMS: 20})

	list, err := FormatList(st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list, "A") || !strings.Contains(list, "B") {
		t.Fatalf("list = %q", list)
	}

	show, err := FormatShow(st, "A")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show, "a1") {
		t.Fatalf("show = %q", show)
	}

	cmp, err := FormatCompare(st, "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmp, "avg total tok") {
		t.Fatalf("cmp = %q", cmp)
	}
}

func TestLoadMissing(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadExperiment("nope"); err == nil {
		t.Fatal("expected not found")
	}
}
