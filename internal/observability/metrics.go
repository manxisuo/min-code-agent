package observability

import (
	"fmt"
	"strings"
	"time"
)

// Metrics aggregates session-level counters from events.
type Metrics struct {
	LLMCalls     int
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	LLMDuration  time.Duration
	Errors       int
}

// MetricsCollector folds events into Metrics. Safe for sequential bus delivery.
type MetricsCollector struct {
	m Metrics
}

// NewMetricsCollector creates an empty collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{}
}

// Handle implements Handler.
func (c *MetricsCollector) Handle(e Event) {
	switch e.Type {
	case EventLLMRequestFinished:
		c.m.LLMCalls++
		if data, ok := asLLMData(e.Data); ok {
			c.m.InputTokens += data.InputTokens
			c.m.OutputTokens += data.OutputTokens
			c.m.TotalTokens += data.TotalTokens
			c.m.LLMDuration += time.Duration(data.DurationMS) * time.Millisecond
		}
	case EventLLMRequestFailed:
		c.m.Errors++
		if data, ok := asLLMData(e.Data); ok {
			c.m.LLMDuration += time.Duration(data.DurationMS) * time.Millisecond
		}
	}
}

// Snapshot returns a copy of current metrics.
func (c *MetricsCollector) Snapshot() Metrics {
	return c.m
}

// Format renders a human-readable metrics block for /metrics.
func (m Metrics) Format() string {
	var b strings.Builder
	b.WriteString("Session Metrics\n\n")
	fmt.Fprintf(&b, "LLM Calls        %d\n", m.LLMCalls)
	fmt.Fprintf(&b, "Errors           %d\n", m.Errors)
	fmt.Fprintf(&b, "Input Tokens     %s\n", formatInt(m.InputTokens))
	fmt.Fprintf(&b, "Output Tokens    %s\n", formatInt(m.OutputTokens))
	fmt.Fprintf(&b, "Total Tokens     %s\n", formatInt(m.TotalTokens))
	fmt.Fprintf(&b, "LLM Time         %s\n", m.LLMDuration.Round(time.Millisecond))
	return b.String()
}

func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	// Insert thousands separators.
	var out []byte
	for i, ch := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, ch)
	}
	return string(out)
}

func asLLMData(v any) (LLMRequestData, bool) {
	switch d := v.(type) {
	case LLMRequestData:
		return d, true
	case *LLMRequestData:
		if d == nil {
			return LLMRequestData{}, false
		}
		return *d, true
	case map[string]any:
		// After JSON round-trip (e.g. if someone re-parses).
		var out LLMRequestData
		if s, ok := d["provider"].(string); ok {
			out.Provider = s
		}
		if s, ok := d["model"].(string); ok {
			out.Model = s
		}
		out.InputTokens = intFromAny(d["input_tokens"])
		out.OutputTokens = intFromAny(d["output_tokens"])
		out.TotalTokens = intFromAny(d["total_tokens"])
		out.DurationMS = int64(intFromAny(d["duration_ms"]))
		return out, true
	}
	return LLMRequestData{}, false
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
