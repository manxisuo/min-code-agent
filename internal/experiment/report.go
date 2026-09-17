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
		fmt.Fprintf(&b, "  %-24s runs=%d success=%d/%d med_steps=%.1f med_tokens=%.0f med_ms=%.0f\n",
			n, agg.Runs, agg.Successes, agg.Runs,
			agg.StepsStat.Median, agg.TotalTokStat.Median, agg.DurationMSStat.Median)
	}
	return b.String(), nil
}

// FormatShow renders one experiment's runs and distribution stats.
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
	fmt.Fprintf(&b, "  failures      %d\n\n", agg.TotalFailures)

	b.WriteString("  metric         min      median      avg        max\n")
	b.WriteString("  " + strings.Repeat("-", 52) + "\n")
	writeStatRow(&b, "steps", agg.StepsStat, agg.AvgSteps)
	writeStatRow(&b, "tool calls", agg.ToolCallsStat, agg.AvgToolCalls)
	writeStatRow(&b, "total tokens", agg.TotalTokStat, agg.AvgTotalTok)
	writeStatRow(&b, "duration ms", agg.DurationMSStat, agg.AvgDurationMS)
	fmt.Fprintf(&b, "  avg LLM calls %.2f   avg in/out %.0f/%.0f   avg compact %.2f\n\n",
		agg.AvgLLMCalls, agg.AvgInTokens, agg.AvgOutTokens, agg.AvgCompactions)

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

func writeStatRow(b *strings.Builder, label string, s Stat, avg float64) {
	fmt.Fprintf(b, "  %-12s %7.0f  %9.0f  %9.1f  %7.0f\n", label, s.Min, s.Median, avg, s.Max)
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
	fmt.Fprintf(&out, "Compare  %s  vs  %s\n", nameA, nameB)
	out.WriteString("(prefer median when runs vary; avg is outlier-sensitive)\n\n")
	out.WriteString(metricRowCmp("metric", nameA, nameB))
	out.WriteString(strings.Repeat("-", 64) + "\n")
	out.WriteString(metricRowCmp("runs", fmt.Sprint(a.Runs), fmt.Sprint(b.Runs)))
	out.WriteString(metricRowCmp("successes", fmt.Sprintf("%d/%d", a.Successes, a.Runs), fmt.Sprintf("%d/%d", b.Successes, b.Runs)))

	out.WriteString(metricRowCmp("steps min", fmtStat0(a.StepsStat.Min), fmtStat0(b.StepsStat.Min)))
	out.WriteString(metricRowCmp("steps median", fmtStat0(a.StepsStat.Median), fmtStat0(b.StepsStat.Median)))
	out.WriteString(metricRowCmp("steps avg", fmt.Sprintf("%.2f", a.AvgSteps), fmt.Sprintf("%.2f", b.AvgSteps)))
	out.WriteString(metricRowCmp("steps max", fmtStat0(a.StepsStat.Max), fmtStat0(b.StepsStat.Max)))

	out.WriteString(metricRowCmp("tools median", fmtStat0(a.ToolCallsStat.Median), fmtStat0(b.ToolCallsStat.Median)))
	out.WriteString(metricRowCmp("LLM calls avg", fmt.Sprintf("%.2f", a.AvgLLMCalls), fmt.Sprintf("%.2f", b.AvgLLMCalls)))

	out.WriteString(metricRowCmp("total tok min", fmtStat0(a.TotalTokStat.Min), fmtStat0(b.TotalTokStat.Min)))
	out.WriteString(metricRowCmp("total tok median", fmtStat0(a.TotalTokStat.Median), fmtStat0(b.TotalTokStat.Median)))
	out.WriteString(metricRowCmp("total tok avg", fmt.Sprintf("%.0f", a.AvgTotalTok), fmt.Sprintf("%.0f", b.AvgTotalTok)))
	out.WriteString(metricRowCmp("total tok max", fmtStat0(a.TotalTokStat.Max), fmtStat0(b.TotalTokStat.Max)))

	out.WriteString(metricRowCmp("duration ms min", fmtStat0(a.DurationMSStat.Min), fmtStat0(b.DurationMSStat.Min)))
	out.WriteString(metricRowCmp("duration ms median", fmtStat0(a.DurationMSStat.Median), fmtStat0(b.DurationMSStat.Median)))
	out.WriteString(metricRowCmp("duration ms avg", fmt.Sprintf("%.0f", a.AvgDurationMS), fmt.Sprintf("%.0f", b.AvgDurationMS)))
	out.WriteString(metricRowCmp("duration ms max", fmtStat0(a.DurationMSStat.Max), fmtStat0(b.DurationMSStat.Max)))

	out.WriteString(metricRowCmp("compactions avg", fmt.Sprintf("%.2f", a.AvgCompactions), fmt.Sprintf("%.2f", b.AvgCompactions)))
	out.WriteString(metricRowCmp("failures", fmt.Sprint(a.TotalFailures), fmt.Sprint(b.TotalFailures)))
	out.WriteString(metricRowCmp("models", orDash(a.Models), orDash(b.Models)))
	return out.String(), nil
}

func metricRowCmp(k, a, b string) string {
	return fmt.Sprintf("  %-20s %-18s %s\n", k, a, b)
}

func fmtStat0(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
