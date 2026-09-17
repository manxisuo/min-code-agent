// Package experiment runs batch A/B trials and records comparable metrics.
package experiment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DirName is the experiments root under the workspace .mincode directory.
const DirName = "experiments"

// Spec describes one named experiment configuration.
type Spec struct {
	Name     string `json:"name"`
	Task     string `json:"task"`
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
	// Repeat is how many sequential runs to execute (default 1).
	Repeat int `json:"repeat,omitempty"`
	// Workspace used for the runs; filled by the runner if empty.
	Workspace string `json:"workspace,omitempty"`
}

// RunResult is the outcome of a single trial.
type RunResult struct {
	RunID         string    `json:"run_id"`
	Experiment    string    `json:"experiment"`
	Task          string    `json:"task"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	Workspace     string    `json:"workspace"`
	Success       bool      `json:"success"`
	Error         string    `json:"error,omitempty"`
	Steps         int       `json:"steps"`
	ToolCalls     int       `json:"tool_calls"`
	LLMCalls      int       `json:"llm_calls"`
	InputTokens   int       `json:"input_tokens"`
	OutputTokens  int       `json:"output_tokens"`
	TotalTokens   int       `json:"total_tokens"`
	Compactions   int       `json:"compactions"`
	Failures      int       `json:"failures"`
	DurationMS    int64     `json:"duration_ms"`
	LLMDurationMS int64     `json:"llm_duration_ms"`
	FinishedAt    time.Time `json:"finished_at"`
	// FinalPreview is a short assistant answer excerpt.
	FinalPreview string `json:"final_preview,omitempty"`
	TracePath    string `json:"trace_path,omitempty"`
}

// Aggregate is the mean/sum summary of all runs under one experiment name.
type Aggregate struct {
	Name           string  `json:"name"`
	Runs           int     `json:"runs"`
	Successes      int     `json:"successes"`
	AvgSteps       float64 `json:"avg_steps"`
	AvgToolCalls   float64 `json:"avg_tool_calls"`
	AvgLLMCalls    float64 `json:"avg_llm_calls"`
	AvgInTokens    float64 `json:"avg_input_tokens"`
	AvgOutTokens   float64 `json:"avg_output_tokens"`
	AvgTotalTok    float64 `json:"avg_total_tokens"`
	AvgDurationMS  float64 `json:"avg_duration_ms"`
	AvgCompactions float64 `json:"avg_compactions"`
	TotalFailures  int     `json:"total_failures"`
	Models         string  `json:"models,omitempty"`
	Tasks          string  `json:"tasks,omitempty"`
}

// Store persists results under <workspace>/.mincode/experiments/<name>/.
type Store struct {
	root string // .../.mincode/experiments
}

// NewStore creates a store under workspace/.mincode/experiments.
func NewStore(workspace string) (*Store, error) {
	if workspace == "" {
		workspace = "."
	}
	root := filepath.Join(workspace, ".mincode", DirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

// Root returns the experiments directory.
func (s *Store) Root() string { return s.root }

// Save writes one run result as JSON. Overwrites if RunID collides.
func (s *Store) Save(r RunResult) error {
	if r.Experiment == "" {
		return fmt.Errorf("experiment name required")
	}
	if r.RunID == "" {
		r.RunID = time.Now().UTC().Format("20060102-150405.000")
	}
	dir := filepath.Join(s.root, r.Experiment)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, r.RunID+".json"), data, 0o644)
}

// LoadExperiment returns all runs for a name, sorted by RunID.
func (s *Store) LoadExperiment(name string) ([]RunResult, error) {
	dir := filepath.Join(s.root, name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("experiment %q not found", name)
		}
		return nil, err
	}
	var out []RunResult
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r RunResult
		if err := json.Unmarshal(data, &r); err != nil {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunID < out[j].RunID })
	return out, nil
}

// List names all experiments that have at least one run file.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		runs, err := s.LoadExperiment(e.Name())
		if err == nil && len(runs) > 0 {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Summarize aggregates runs into one Aggregate.
func Summarize(name string, runs []RunResult) Aggregate {
	agg := Aggregate{Name: name, Runs: len(runs)}
	if len(runs) == 0 {
		return agg
	}
	models := map[string]bool{}
	tasks := map[string]bool{}
	var steps, tools, llm, inTok, outTok, totTok, dur, comp int
	for _, r := range runs {
		if r.Success {
			agg.Successes++
		}
		steps += r.Steps
		tools += r.ToolCalls
		llm += r.LLMCalls
		inTok += r.InputTokens
		outTok += r.OutputTokens
		totTok += r.TotalTokens
		dur += int(r.DurationMS)
		comp += r.Compactions
		agg.TotalFailures += r.Failures
		if r.Model != "" {
			models[r.Model] = true
		}
		if r.Task != "" {
			tasks[truncate(r.Task, 40)] = true
		}
	}
	n := float64(len(runs))
	agg.AvgSteps = float64(steps) / n
	agg.AvgToolCalls = float64(tools) / n
	agg.AvgLLMCalls = float64(llm) / n
	agg.AvgInTokens = float64(inTok) / n
	agg.AvgOutTokens = float64(outTok) / n
	agg.AvgTotalTok = float64(totTok) / n
	agg.AvgDurationMS = float64(dur) / n
	agg.AvgCompactions = float64(comp) / n
	agg.Models = joinKeys(models)
	agg.Tasks = joinKeys(tasks)
	return agg
}

func joinKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
