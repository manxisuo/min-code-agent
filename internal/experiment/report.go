package experiment

import (
	"fmt"
	"strings"
)

// FormatList renders experiment names with run counts.
func FormatList(store *Store) (string, error) {
	names, err := store.List()
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "no experiments yet — use: mincode experiment run --name <label> --task \"...\"\n", nil
	}
	var b strings.Builder
	b.WriteString("Experiments\n\n")
	for _, n := range names {
		runs, err := store.LoadExperiment(n)
		if err != nil {
			continue
		}
		agg := Summarize(n, runs)
		fmt.Fprintf(&b, "  %-24s runs=%d success=%d/%d avg_tokens=%.0f avg_ms=%.0f\n",
			n, agg.Runs, agg.Successes, agg.Runs, agg.AvgTotalTok, agg.AvgDurationMS)
	}
	return b.String(), nil
}

// FormatShow renders one experiment's runs.
func FormatShow(store *Store, name string) (string, error) {
	runs, err := store.LoadExperiment(name)
	if err != nil {
		return "", err
	}
	agg := Summarize(name, runs)
	var b strings.Builder
	fmt.Fprintf(&b, "Experiment %s\n\n", name)
	fmt.Fprintf(&b, "  runs          %d\n", agg.Runs)
	fmt.Fprintf(&b, "  successes     %d\n", agg.Successes)
	fmt.Fprintf(&b, "  models        %s\n", orDash(agg.Models))
	fmt.Fprintf(&b, "  avg steps     %.2f\n", agg.AvgSteps)
	fmt.Fprintf(&b, "  avg tools     %.2f\n", agg.AvgToolCalls)
	fmt.Fprintf(&b, "  avg LLM calls %.2f\n", agg.AvgLLMCalls)
	fmt.Fprintf(&b, "  avg in/out    %.0f / %.0f tokens\n", agg.AvgInTokens, agg.AvgOutTokens)
	fmt.Fprintf(&b, "  avg total     %.0f tokens\n", agg.AvgTotalTok)
	fmt.Fprintf(&b, "  avg duration  %.0f ms\n", agg.AvgDurationMS)
	fmt.Fprintf(&b, "  avg compact   %.2f\n", agg.AvgCompactions)
	fmt.Fprintf(&b, "  failures      %d\n\n", agg.TotalFailures)

	b.WriteString("  run_id                 ok  steps tools llm   in_tok  out_tok  total   ms\n")
	for _, r := range runs {
		ok := "n"
		if r.Success {
			ok = "y"
		}
		fmt.Fprintf(&b, "  %-22s %s  %5d %5d %3d %7d %8d %6d %5d\n",
			r.RunID, ok, r.Steps, r.ToolCalls, r.LLMCalls, r.InputTokens, r.OutputTokens, r.TotalTokens, r.DurationMS)
		if r.Error != "" {
			fmt.Fprintf(&b, "      error: %s\n", truncate(r.Error, 80))
		}
	}
	return b.String(), nil
}

// FormatCompare renders two experiment aggregates side by side.
func FormatCompare(store *Store, nameA, nameB string) (string, error) {
	runsA, err := store.LoadExperiment(nameA)
	if err != nil {
		return "", err
	}
	runsB, err := store.LoadExperiment(nameB)
	if err != nil {
		return "", err
	}
	a := Summarize(nameA, runsA)
	b := Summarize(nameB, runsB)

	var out strings.Builder
	fmt.Fprintf(&out, "Compare  %s  vs  %s\n\n", nameA, nameB)
	out.WriteString(metricRowCmp("metric", nameA, nameB))
	out.WriteString(strings.Repeat("-", 64) + "\n")
	out.WriteString(metricRowCmp("runs", fmt.Sprint(a.Runs), fmt.Sprint(b.Runs)))
	out.WriteString(metricRowCmp("successes", fmt.Sprintf("%d/%d", a.Successes, a.Runs), fmt.Sprintf("%d/%d", b.Successes, b.Runs)))
	out.WriteString(metricRowCmp("avg steps", fmt.Sprintf("%.2f", a.AvgSteps), fmt.Sprintf("%.2f", b.AvgSteps)))
	out.WriteString(metricRowCmp("avg tool calls", fmt.Sprintf("%.2f", a.AvgToolCalls), fmt.Sprintf("%.2f", b.AvgToolCalls)))
	out.WriteString(metricRowCmp("avg LLM calls", fmt.Sprintf("%.2f", a.AvgLLMCalls), fmt.Sprintf("%.2f", b.AvgLLMCalls)))
	out.WriteString(metricRowCmp("avg input tok", fmt.Sprintf("%.0f", a.AvgInTokens), fmt.Sprintf("%.0f", b.AvgInTokens)))
	out.WriteString(metricRowCmp("avg output tok", fmt.Sprintf("%.0f", a.AvgOutTokens), fmt.Sprintf("%.0f", b.AvgOutTokens)))
	out.WriteString(metricRowCmp("avg total tok", fmt.Sprintf("%.0f", a.AvgTotalTok), fmt.Sprintf("%.0f", b.AvgTotalTok)))
	out.WriteString(metricRowCmp("avg duration ms", fmt.Sprintf("%.0f", a.AvgDurationMS), fmt.Sprintf("%.0f", b.AvgDurationMS)))
	out.WriteString(metricRowCmp("avg compactions", fmt.Sprintf("%.2f", a.AvgCompactions), fmt.Sprintf("%.2f", b.AvgCompactions)))
	out.WriteString(metricRowCmp("failures", fmt.Sprint(a.TotalFailures), fmt.Sprint(b.TotalFailures)))
	out.WriteString(metricRowCmp("models", orDash(a.Models), orDash(b.Models)))
	return out.String(), nil
}

func metricRowCmp(k, a, b string) string {
	return fmt.Sprintf("  %-18s %-18s %s\n", k, a, b)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
