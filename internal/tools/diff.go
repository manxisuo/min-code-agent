package tools

import (
	"fmt"
	"strings"
)

// UnifiedDiff renders a simple unified-diff-like view of old→new text
// using common prefix/suffix compression (good enough for exact-replace edits).
func UnifiedDiff(path, oldText, newText string, contextLines int) string {
	if contextLines < 0 {
		contextLines = 3
	}
	oldLines := splitKeepEmpty(oldText)
	newLines := splitKeepEmpty(newText)

	lo := 0
	for lo < len(oldLines) && lo < len(newLines) && oldLines[lo] == newLines[lo] {
		lo++
	}
	ro, rn := len(oldLines), len(newLines)
	for ro > lo && rn > lo && oldLines[ro-1] == newLines[rn-1] {
		ro--
		rn--
	}

	start := lo - contextLines
	if start < 0 {
		start = 0
	}
	endOld := ro + contextLines
	if endOld > len(oldLines) {
		endOld = len(oldLines)
	}

	oldCount := endOld - start
	// New hunk length: prefix context + inserted + suffix context from new.
	endNew := rn + contextLines
	if endNew > len(newLines) {
		endNew = len(newLines)
	}
	if endNew < lo {
		endNew = lo
	}
	newCount := (lo - start) + (rn - lo) + (endNew - rn)
	if newCount < 0 {
		newCount = 0
	}

	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", path, path)
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", start+1, oldCount, start+1, newCount)

	for i := start; i < lo; i++ {
		b.WriteString("  " + oldLines[i] + "\n")
	}
	for i := lo; i < ro; i++ {
		b.WriteString("- " + oldLines[i] + "\n")
	}
	for i := lo; i < rn; i++ {
		b.WriteString("+ " + newLines[i] + "\n")
	}
	for i := ro; i < endOld; i++ {
		b.WriteString("  " + oldLines[i] + "\n")
	}

	out := b.String()
	if len(out) > 16*1024 {
		out = out[:16*1024] + "\n... (diff truncated)\n"
	}
	return out
}

func splitKeepEmpty(s string) []string {
	if s == "" {
		return []string{}
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.Split(s, "\n")
	if n := len(parts); n > 0 && parts[n-1] == "" {
		parts = parts[:n-1]
	}
	return parts
}
