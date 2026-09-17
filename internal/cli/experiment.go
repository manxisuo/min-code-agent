package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manxisuo/mincode/internal/experiment"
)

// RunExperimentCLI dispatches `mincode experiment ...` subcommands.
// argv is os.Args[1:] after the "experiment" token.
func RunExperimentCLI(argv []string, out io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprint(out, experimentUsage)
		return 2
	}
	switch argv[0] {
	case "run":
		return experimentRun(argv[1:], out)
	case "list":
		return experimentList(argv[1:], out)
	case "show":
		return experimentShow(argv[1:], out)
	case "compare":
		return experimentCompare(argv[1:], out)
	case "help", "-h", "--help":
		fmt.Fprint(out, experimentUsage)
		return 0
	default:
		fmt.Fprintf(out, "unknown experiment command %q\n", argv[0])
		fmt.Fprint(out, experimentUsage)
		return 2
	}
}

const experimentUsage = `Usage:
  mincode experiment run --name <label> --task <prompt> [flags]
  mincode experiment list
  mincode experiment show <name>
  mincode experiment compare <nameA> <nameB>

Run flags:
  --name        experiment label (required)
  --task        task prompt (required)
  --model       override model
  --provider    override provider type
  --repeat      sequential runs (default 1)
  --workspace   workspace directory (default: cwd)
  --config      mincode.yaml path

Results: <workspace>/.mincode/experiments/<name>/<run-id>.json
`

func experimentRun(argv []string, out io.Writer) int {
	fs := flag.NewFlagSet("experiment run", flag.ContinueOnError)
	fs.SetOutput(out)
	var spec experiment.Spec
	var workspace, configPath string
	fs.StringVar(&spec.Name, "name", "", "experiment label")
	fs.StringVar(&spec.Task, "task", "", "task prompt")
	fs.StringVar(&spec.Model, "model", "", "model override")
	fs.StringVar(&spec.Provider, "provider", "", "provider override")
	fs.IntVar(&spec.Repeat, "repeat", 1, "number of runs")
	fs.StringVar(&workspace, "workspace", "", "workspace directory")
	fs.StringVar(&configPath, "config", "", "config path")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if spec.Name == "" || spec.Task == "" {
		fmt.Fprintln(out, "error: --name and --task are required")
		fmt.Fprint(out, experimentUsage)
		return 2
	}
	if spec.Repeat <= 0 {
		spec.Repeat = 1
	}
	if workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			workspace = wd
		} else {
			workspace = "."
		}
	}
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}
	spec.Workspace = workspace

	store, err := experiment.NewStore(workspace)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}

	fmt.Fprintf(out, "experiment %s  task=%q  repeat=%d\n\n", spec.Name, truncateRunes(spec.Task, 60), spec.Repeat)

	ctx := context.Background()
	okRuns := 0
	for i := 0; i < spec.Repeat; i++ {
		fmt.Fprintf(out, "── run %d/%d\n", i+1, spec.Repeat)
		res := executeExperimentRun(ctx, workspace, configPath, spec, i+1)
		if err := store.Save(res); err != nil {
			fmt.Fprintf(out, "error: save result: %v\n", err)
			return 1
		}
		if res.Success {
			okRuns++
			fmt.Fprintf(out, "   ok  steps=%d tools=%d llm=%d tokens=%d duration=%dms\n",
				res.Steps, res.ToolCalls, res.LLMCalls, res.TotalTokens, res.DurationMS)
		} else {
			fmt.Fprintf(out, "   fail  %s\n", res.Error)
		}
		if res.TracePath != "" {
			fmt.Fprintf(out, "   trace %s\n", res.TracePath)
		}
		fmt.Fprintln(out)
	}

	fmt.Fprintf(out, "saved %d/%d successful runs under %s\n",
		okRuns, spec.Repeat, filepath.Join(store.Root(), spec.Name))
	if okRuns == 0 {
		return 1
	}
	return 0
}

// executeExperimentRun builds a fresh App, runs the task once, and harvests metrics.
func executeExperimentRun(ctx context.Context, workspace, configPath string, spec experiment.Spec, index int) experiment.RunResult {
	runID := fmt.Sprintf("%s-%03d", time.Now().UTC().Format("20060102-150405"), index)
	res := experiment.RunResult{
		RunID:      runID,
		Experiment: spec.Name,
		Task:       spec.Task,
		Workspace:  workspace,
		FinishedAt: time.Now().UTC(),
	}

	opts := Options{
		ConfigPath: configPath,
		Workspace:  workspace,
		Model:      spec.Model,
		Provider:   spec.Provider,
	}
	app, err := NewApp(opts)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer app.Close()

	// Silence per-run REPL noise; keep a buffer if needed later.
	sink := io.Discard
	app.SetOutput(sink)

	prov, model := app.ProviderInfo()
	res.Provider = prov
	res.Model = model

	start := time.Now()
	runErr := app.RunPrompt(ctx, spec.Task)
	res.DurationMS = time.Since(start).Milliseconds()
	res.TracePath = app.TracePath()

	m := app.MetricsSnapshot()
	res.LLMCalls = m.LLMCalls
	res.InputTokens = m.InputTokens
	res.OutputTokens = m.OutputTokens
	res.TotalTokens = m.TotalTokens
	res.LLMDurationMS = m.LLMDuration.Milliseconds()
	res.Failures = m.Errors

	if last := app.LastResult(); last != nil {
		res.Steps = last.Steps
		res.ToolCalls = last.ToolCalls
		res.FinalPreview = truncateRunes(strings.TrimSpace(last.Final), 200)
	}

	if runErr != nil {
		res.Error = runErr.Error()
		res.Success = false
		return res
	}
	res.Success = true
	return res
}

func experimentList(argv []string, out io.Writer) int {
	workspace := workspaceFromArgs(argv)
	store, err := experiment.NewStore(workspace)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}
	s, err := experiment.FormatList(store)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}
	fmt.Fprint(out, s)
	return 0
}

func experimentShow(argv []string, out io.Writer) int {
	if len(argv) < 1 {
		fmt.Fprintln(out, "usage: mincode experiment show <name> [--workspace DIR]")
		return 2
	}
	name := argv[0]
	workspace := workspaceFromArgs(argv[1:])
	store, err := experiment.NewStore(workspace)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}
	s, err := experiment.FormatShow(store, name)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}
	fmt.Fprint(out, s)
	return 0
}

func experimentCompare(argv []string, out io.Writer) int {
	if len(argv) < 2 {
		fmt.Fprintln(out, "usage: mincode experiment compare <nameA> <nameB> [--workspace DIR]")
		return 2
	}
	nameA, nameB := argv[0], argv[1]
	workspace := workspaceFromArgs(argv[2:])
	store, err := experiment.NewStore(workspace)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}
	s, err := experiment.FormatCompare(store, nameA, nameB)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}
	fmt.Fprint(out, s)
	return 0
}

// workspaceFromArgs extracts --workspace DIR (or -workspace) from leftover args.
func workspaceFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--workspace" || a == "-workspace" {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
		if strings.HasPrefix(a, "--workspace=") {
			return strings.TrimPrefix(a, "--workspace=")
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
