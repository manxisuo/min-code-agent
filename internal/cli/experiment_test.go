package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExperimentRunListCompare(t *testing.T) {
	ws := t.TempDir()
	cfg := filepath.Join(ws, "mincode.yaml")
	yaml := "provider:\n  type: fake\n  model: fake-model\n"
	if err := os.WriteFile(cfg, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := RunExperimentCLI([]string{
		"run",
		"--name", "baseline",
		"--task", "say hello",
		"--workspace", ws,
		"--config", cfg,
		"--provider", "fake",
		"--repeat", "2",
	}, &out)
	if code != 0 {
		t.Fatalf("run exit=%d out=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "saved 2/2") {
		t.Fatalf("out = %s", out.String())
	}

	// Second experiment
	out.Reset()
	code = RunExperimentCLI([]string{
		"run",
		"--name", "alt",
		"--task", "say hello",
		"--workspace", ws,
		"--config", cfg,
		"--provider", "fake",
		"--repeat", "1",
	}, &out)
	if code != 0 {
		t.Fatalf("alt exit=%d out=%s", code, out.String())
	}

	out.Reset()
	code = RunExperimentCLI([]string{"list", "--workspace", ws}, &out)
	if code != 0 || !strings.Contains(out.String(), "baseline") || !strings.Contains(out.String(), "alt") {
		t.Fatalf("list exit=%d out=%s", code, out.String())
	}

	out.Reset()
	code = RunExperimentCLI([]string{"show", "baseline", "--workspace", ws}, &out)
	if code != 0 || !strings.Contains(out.String(), "runs") {
		t.Fatalf("show exit=%d out=%s", code, out.String())
	}

	out.Reset()
	code = RunExperimentCLI([]string{"compare", "baseline", "alt", "--workspace", ws}, &out)
	if code != 0 || !strings.Contains(out.String(), "avg total tok") {
		t.Fatalf("compare exit=%d out=%s", code, out.String())
	}

	// Result files exist
	if _, err := os.Stat(filepath.Join(ws, ".mincode", "experiments", "baseline")); err != nil {
		t.Fatal(err)
	}
}

func TestExperimentRunRequiresNameTask(t *testing.T) {
	var out bytes.Buffer
	if code := RunExperimentCLI([]string{"run", "--task", "x"}, &out); code == 0 {
		t.Fatal("expected non-zero exit")
	}
}
