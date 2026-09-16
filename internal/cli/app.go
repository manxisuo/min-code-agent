package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/mincode/mincode/internal/agent"
	"github.com/mincode/mincode/internal/config"
	"github.com/mincode/mincode/internal/ctxmgr"
	"github.com/mincode/mincode/internal/llm"
	"github.com/mincode/mincode/internal/observability"
	"github.com/mincode/mincode/internal/tools"
)

// Options are runtime options from flags.
type Options struct {
	ConfigPath string
	Prompt     string // single-shot mode when non-empty
	Model      string // override
	Provider   string // override
	Workspace  string
}

// App wires config, provider, agent, observability and the REPL.
type App struct {
	cfg       config.Config
	provider  llm.Provider
	agent     *agent.Agent
	bus       *observability.Bus
	recorder  *observability.Recorder
	metrics   *observability.MetricsCollector
	sessionID string
	workspace string
	out       io.Writer
	echoTools atomic.Bool
}

// NewApp constructs the application from options.
func NewApp(opts Options) (*App, error) {
	// Resolve workspace first so mincode.yaml can be loaded from it.
	workspace := opts.Workspace
	if workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			workspace = "."
		} else {
			workspace = wd
		}
	}
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}

	cfg, err := config.LoadFrom(opts.ConfigPath, workspace)
	if err != nil {
		return nil, err
	}
	if opts.Model != "" {
		cfg.Provider.Model = opts.Model
	}
	if opts.Provider != "" {
		cfg.Provider.Type = opts.Provider
	}

	provider, err := buildProvider(cfg)
	if err != nil {
		return nil, err
	}

	sessionID := time.Now().UTC().Format("20060102-150405") + "-" + observability.NewID()[:8]
	if err := config.EnsureTraceDir(cfg.Trace.Dir); err != nil {
		return nil, err
	}
	tracePath := config.TracePath(cfg.Trace.Dir, sessionID)
	recorder, err := observability.NewRecorder(tracePath)
	if err != nil {
		return nil, err
	}

	bus := observability.NewBus()
	metrics := observability.NewMetricsCollector()
	bus.Subscribe(func(e observability.Event) {
		if err := recorder.Record(e); err != nil {
			fmt.Fprintf(os.Stderr, "trace write error: %v\n", err)
		}
	})
	bus.Subscribe(metrics.Handle)

	ws, err := tools.NewWorkspace(workspace)
	if err != nil {
		_ = recorder.Close()
		return nil, err
	}
	registry := tools.NewRegistry()
	registry.Register(&tools.ReadFile{WS: ws})
	registry.Register(&tools.ListDir{WS: ws})
	registry.Register(&tools.Glob{WS: ws})
	registry.Register(&tools.Grep{WS: ws})

	ag := agent.New(provider, registry, bus, sessionID, cfg.Agent.MaxSteps, cfg.Agent.SystemPrompt, cfg.Agent.TokenBudget)

	app := &App{
		cfg:       cfg,
		provider:  provider,
		agent:     ag,
		bus:       bus,
		recorder:  recorder,
		metrics:   metrics,
		sessionID: sessionID,
		workspace: workspace,
		out:       os.Stdout,
	}
	bus.Subscribe(func(e observability.Event) {
		if !app.echoTools.Load() {
			return
		}
		echoToolEvent(app.out, e)
	})

	app.emit(observability.EventSessionCreated, observability.SessionCreatedData{
		Workspace: workspace,
		Model:     provider.Model(),
		Provider:  provider.Name(),
	})
	return app, nil
}

func buildProvider(cfg config.Config) (llm.Provider, error) {
	switch cfg.Provider.Type {
	case "fake":
		return llm.NewFakeProvider(cfg.Provider.Model,
			"This is a scripted Fake Provider response (offline mode).",
		), nil
	case "openai-compatible", "openai", "":
		return llm.NewCompatibleProvider(
			cfg.Provider.BaseURL,
			cfg.Provider.APIKey,
			cfg.Provider.Model,
			cfg.Provider.Temperature,
			cfg.Provider.MaxTokens,
			cfg.Provider.TimeoutSec,
		), nil
	default:
		return nil, fmt.Errorf("unknown provider type %q", cfg.Provider.Type)
	}
}

// Close releases the trace file.
func (a *App) Close() error {
	if a.recorder != nil {
		return a.recorder.Close()
	}
	return nil
}

// SessionID returns the current session id.
func (a *App) SessionID() string { return a.sessionID }

// TracePath returns the current JSONL trace path.
func (a *App) TracePath() string { return a.recorder.Path() }

func (a *App) emit(typ observability.EventType, data any) {
	a.bus.Publish(observability.NewEvent(a.sessionID, 0, typ, data))
}

// Run starts the app: single-shot if opts.Prompt is set, else REPL.
func (a *App) Run(ctx context.Context, opts Options) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.Prompt != "" {
		return a.singleShot(ctx, opts.Prompt)
	}
	return a.repl(ctx)
}

func (a *App) singleShot(ctx context.Context, prompt string) error {
	a.emit(observability.EventAgentStarted, observability.AgentLifecycleData{Reason: "single-shot"})
	res, err := a.agent.Run(ctx, prompt)
	if err != nil {
		a.emit(observability.EventAgentFailed, observability.AgentLifecycleData{Reason: err.Error()})
		return err
	}
	if res.Final != "" {
		fmt.Fprintln(a.out, res.Final)
	}
	a.emit(observability.EventAgentFinished, observability.AgentLifecycleData{
		Reason: fmt.Sprintf("single-shot complete steps=%d tools=%d", res.Steps, res.ToolCalls),
	})
	return nil
}

func (a *App) repl(ctx context.Context) error {
	fmt.Fprintf(a.out, "%s  %s\n", bold(cyan("Min Code Agent")), bannerLine("session", a.sessionID))
	fmt.Fprintf(a.out, "%s  %s  %s\n", bannerLine("provider", a.provider.Name()), bannerLine("model", a.provider.Model()), bannerLine("workspace", a.workspace))
	fmt.Fprintf(a.out, "%s\n", bannerLine("tools", strings.Join(a.toolNames(), ", ")))
	fmt.Fprintf(a.out, "%s\n", bannerLine("trace", a.recorder.Path()))
	fmt.Fprintf(a.out, "Type a message, or %s for commands.\n\n", bold("/help"))

	a.emit(observability.EventAgentStarted, observability.AgentLifecycleData{Reason: "repl"})

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for {
		fmt.Fprint(a.out, promptString())
		if !in.Scan() {
			break
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			if quit := a.handleCommand(line); quit {
				break
			}
			continue
		}

		if err := a.runTurn(ctx, line); err != nil {
			fmt.Fprintf(a.out, "error: %v\n", err)
		}
	}

	if err := in.Err(); err != nil {
		return err
	}
	a.emit(observability.EventAgentFinished, observability.AgentLifecycleData{Reason: "repl exit"})
	return nil
}

func (a *App) toolNames() []string {
	// Registry is inside agent; expose via a lightweight re-query if needed.
	// For display we hardcode the Phase 2 set registered in NewApp.
	return []string{"read_file", "list_dir", "glob", "grep"}
}

// runTurn executes one user turn through the agent and prints tool + final output.
func (a *App) runTurn(ctx context.Context, userText string) error {
	a.echoTools.Store(true)
	defer a.echoTools.Store(false)

	res, err := a.agent.Run(ctx, userText)
	if err != nil {
		return err
	}
	if res.Final != "" {
		fmt.Fprintln(a.out)
		fmt.Fprintln(a.out, res.Final)
		fmt.Fprintln(a.out)
	} else {
		fmt.Fprintln(a.out)
	}
	return nil
}

func echoToolEvent(w io.Writer, e observability.Event) {
	switch e.Type {
	case observability.EventToolStarted:
		if data, ok := toolData(e.Data); ok {
			fmt.Fprintf(w, "  %s %s %s\n", cyan("→"), bold(data.Tool), gray(compactJSON(data.Arguments)))
		}
	case observability.EventToolFinished, observability.EventToolFailed:
		if data, ok := toolData(e.Data); ok {
			status := green("ok")
			if data.IsError || e.Type == observability.EventToolFailed {
				status = red("error")
			}
			dur := time.Duration(data.DurationMS) * time.Millisecond
			fmt.Fprintf(w, "  %s %s %s %s\n",
				dim("←"),
				status,
				bold(data.Tool),
				gray(fmt.Sprintf("%dB %s", data.ResultSize, dur.Round(time.Millisecond))),
			)
		}
	}
}

func toolData(v any) (observability.ToolEventData, bool) {
	switch d := v.(type) {
	case observability.ToolEventData:
		return d, true
	case map[string]any:
		out := observability.ToolEventData{}
		if s, ok := d["tool"].(string); ok {
			out.Tool = s
		}
		if s, ok := d["arguments"].(string); ok {
			out.Arguments = s
		}
		if s, ok := d["error"].(string); ok {
			out.Error = s
		}
		if b, ok := d["is_error"].(bool); ok {
			out.IsError = b
		}
		out.ResultSize = intFromAny(d["result_size"])
		out.DurationMS = int64(intFromAny(d["duration_ms"]))
		return out, true
	}
	return observability.ToolEventData{}, false
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

func compactJSON(s string) string {
	if len(s) > 120 {
		s = truncateUTF8(s, 120)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", " "); err != nil {
		return s
	}
	out := strings.ReplaceAll(buf.String(), "\n", " ")
	if len(out) > 120 {
		return truncateUTF8(out, 120)
	}
	return out
}

// truncateUTF8 cuts s to at most max bytes on a rune boundary and appends "...".
func truncateUTF8(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "..."
}

func (a *App) handleCommand(line string) (quit bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	name := fields[0]

	switch name {
	case "/help", "/h", "/?":
		fmt.Fprint(a.out, `Commands:
  /help              show this help
  /timeline          show agent execution timeline
  /context           show last context snapshot (what the model saw)
  /trace [n]         show last n raw trace events (default 30)
  /metrics           show session token/time metrics
  /clear             clear conversation history
  /exit, /quit       leave the REPL

Trace file:
`)
		fmt.Fprintf(a.out, "  %s\n", a.recorder.Path())
	case "/timeline":
		a.printTimeline()
	case "/context":
		a.printContextSnapshot()
	case "/trace":
		n := 30
		if len(fields) > 1 {
			if v, err := atoi(fields[1]); err == nil && v > 0 {
				n = v
			}
		}
		a.printTrace(n)
	case "/metrics":
		fmt.Fprintln(a.out)
		fmt.Fprint(a.out, formatMetrics(a.metrics.Snapshot()))
		fmt.Fprintln(a.out)
	case "/clear":
		a.agent.ClearConversation()
		fmt.Fprintln(a.out, "history cleared (system prompt kept)")
	case "/exit", "/quit":
		return true
	default:
		fmt.Fprintf(a.out, "unknown command %s (try /help)\n", name)
	}
	return false
}

func formatMetrics(m observability.Metrics) string {
	var b strings.Builder
	b.WriteString(bold(cyan("Session Metrics")) + "\n\n")
	b.WriteString(metricRow("LLM Calls", green(fmt.Sprint(m.LLMCalls)), ""))
	errVal := green(fmt.Sprint(m.Errors))
	if m.Errors > 0 {
		errVal = red(fmt.Sprint(m.Errors))
	}
	b.WriteString(metricRow("Errors", errVal, ""))
	b.WriteString(metricRow("Input Tokens", yellow(formatInt(m.InputTokens)), ""))
	b.WriteString(metricRow("Output Tokens", yellow(formatInt(m.OutputTokens)), ""))
	b.WriteString(metricRow("Total Tokens", bold(yellow(formatInt(m.TotalTokens))), ""))
	b.WriteString(metricRow("LLM Time", blue(m.LLMDuration.Round(time.Millisecond).String()), ""))
	return b.String()
}

func metricRow(label, value, unit string) string {
	line := padCol(cyan(label), 18) + value
	if unit != "" {
		line += " " + gray(unit)
	}
	return line + "\n"
}

func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	var out []byte
	for i, ch := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, ch)
	}
	return string(out)
}

func (a *App) printContextSnapshot() {
	snap := a.agent.Ctx.LastSnapshot()
	if snap == nil {
		fmt.Fprint(a.out, "\n"+yellow("no context snapshot yet")+" — run a prompt first\n\n")
		return
	}
	fmt.Fprintln(a.out)
	fmt.Fprint(a.out, formatSnapshot(*snap))
	fmt.Fprintln(a.out)
}

func formatSnapshot(s ctxmgr.Snapshot) string {
	var b strings.Builder
	b.WriteString(bold("Context Snapshot #"+fmt.Sprint(s.Step)) + "\n\n")
	b.WriteString(padCol(cyan("Source"), 14) + padCol(cyan("Role"), 12) + padCol(cyan("Tokens"), 8) + cyan("Status") + "\n")
	b.WriteString(gray(strings.Repeat("─", 64)) + "\n")

	for _, it := range s.Items {
		src := paintSource(string(it.Source))
		role := gray(it.Role)
		toks := fmt.Sprint(it.Tokens)
		status := green("included")
		switch {
		case it.Excluded:
			status = yellow("excluded")
			if it.Reason != "" {
				status += gray(" (" + it.Reason + ")")
			}
		case it.Truncated:
			status = yellow("truncated")
			if it.Reason != "" {
				status += gray(" (" + it.Reason + ")")
			}
		}
		if it.Pinned && !it.Excluded {
			status += blue(" [pinned]")
		}
		b.WriteString(padCol(src, 14) + padCol(role, 12) + padCol(toks, 8) + status + "\n")
		preview := it.Preview
		if preview == "" {
			preview = "-"
		}
		b.WriteString("             " + dim(truncateStr(preview, 56)) + "\n")
	}
	b.WriteString(gray(strings.Repeat("─", 64)) + "\n")
	b.WriteString(padCol("Included", 14) + padCol("", 12) + padCol(fmt.Sprint(s.Included), 8) + "items\n")
	b.WriteString(padCol(yellow("Excluded"), 14) + padCol("", 12) + padCol(fmt.Sprint(s.Excluded), 8) + "items\n")
	b.WriteString(padCol(yellow("Truncated"), 14) + padCol("", 12) + padCol(fmt.Sprint(s.Truncated), 8) + "items\n")
	if s.ToolTokens > 0 {
		b.WriteString(padCol(magenta("Tool schemas"), 14) + padCol("", 12) + padCol(magenta(fmt.Sprint(s.ToolTokens)), 8) +
			gray("tokens (sent every call, not chat messages)") + "\n")
	}
	grand := s.TotalTokens + s.ToolTokens
	totalCol := green(fmt.Sprint(grand))
	if grand > s.Budget*9/10 {
		totalCol = yellow(fmt.Sprint(grand))
	}
	b.WriteString(padCol(bold("Total"), 14) + padCol("", 12) + padCol(totalCol, 8) +
		"tokens / budget " + fmt.Sprint(s.Budget) + "\n")
	if s.ActualPromptTokens > 0 {
		estTotal := grand
		ratio := s.EstimateRatio
		if ratio <= 0 && estTotal > 0 {
			ratio = float64(s.ActualPromptTokens) / float64(estTotal)
		}
		delta := s.ActualPromptTokens - estTotal
		sign := "+"
		if delta < 0 {
			sign = ""
		}
		b.WriteString(padCol(blue("API actual"), 14) + padCol("", 12) + padCol(blue(fmt.Sprint(s.ActualPromptTokens)), 8) +
			gray(fmt.Sprintf("prompt_tokens  ratio=%.2f  Δ%s%d", ratio, sign, delta)) + "\n")
	}
	return b.String()
}

func paintSource(src string) string {
	switch src {
	case "system":
		return bold(magenta(src))
	case "instructions":
		return magenta(src)
	case "user_input":
		return green(src)
	case "history":
		return cyan(src)
	case "tool_result":
		return blue(src)
	case "pinned":
		return yellow(src)
	default:
		return src
	}
}

func padCol(s string, n int) string {
	// Pad using visible width (strip ANSI).
	vis := len(stripANSI(s))
	if vis >= n {
		return s + " "
	}
	return s + strings.Repeat(" ", n-vis)
}

func (a *App) printTimeline() {
	events, err := observability.ReadEvents(a.recorder.Path())
	if err != nil {
		fmt.Fprintf(a.out, "%s %v\n", red("read trace:"), err)
		return
	}
	fmt.Fprintf(a.out, "\n%s  %s\n\n", bold("Timeline"), gray(a.recorder.Path()))
	seq := 0
	for _, e := range events {
		line, ok := timelineLine(e)
		if !ok {
			continue
		}
		seq++
		fmt.Fprintf(a.out, "%s  %s\n", gray(fmt.Sprintf("%3d", seq)), line)
	}
	fmt.Fprintln(a.out)
}

func timelineLine(e observability.Event) (string, bool) {
	ts := gray(e.Time.Local().Format("15:04:05.000"))
	switch e.Type {
	case observability.EventSessionCreated:
		return ts + "  " + bold(blue("Session Created")), true
	case observability.EventAgentStarted:
		return ts + "  " + bold(green("Agent Started")), true
	case observability.EventAgentFinished:
		return ts + "  " + bold(green("Agent Finished")), true
	case observability.EventAgentFailed:
		return ts + "  " + bold(red("Agent Failed")), true
	case observability.EventAgentStateChanged:
		data := mapFromAny(e.Data)
		to, _ := data["to"].(string)
		switch to {
		case "BUILDING_CONTEXT", "CALLING_LLM", "PROCESSING_RESPONSE", "EXECUTING_TOOL",
			"FINISHED", "FAILED", "CANCELLED", "MAX_STEPS_REACHED":
			return ts + "  " + dim("State →") + " " + paintState(to), true
		}
		return "", false
	case observability.EventLLMRequestStarted:
		data := mapFromAny(e.Data)
		return fmt.Sprintf("%s  %s %s %s",
			ts, magenta("LLM Request"), fmt.Sprint(data["model"]), gray(fmt.Sprint("messages=", data["message_count"]))), true
	case observability.EventContextBuilt:
		data := mapFromAny(e.Data)
		est := fmt.Sprint(data["total_tokens"])
		if v, ok := data["excluded_count"].(float64); ok && v > 0 {
			est = yellow(est)
		} else {
			est = green(est)
		}
		toolsTok := data["tool_tokens"]
		if toolsTok == nil {
			toolsTok = 0
		}
		return fmt.Sprintf("%s  %s msgs=%s %s tools=%v excl=%v trunc=%v",
			ts, cyan("Context Built"), est, gray("budget="+fmt.Sprint(data["budget"])),
			toolsTok, data["excluded_count"], data["truncated_count"]), true
	case observability.EventLLMRequestFinished:
		data := mapFromAny(e.Data)
		preview, _ := data["content_preview"].(string)
		// Flatten newlines so a multi-line poem cannot break the timeline grid.
		preview = strings.ReplaceAll(preview, "\r\n", "\n")
		preview = strings.ReplaceAll(preview, "\n", " ↵ ")
		preview = truncateStr(preview, 72)
		head := fmt.Sprintf("%s  %s %s %s %s",
			ts, magenta("LLM Response"),
			gray(fmt.Sprintf("in=%v out=%v", data["input_tokens"], data["output_tokens"])),
			gray(fmt.Sprintf("%vms", data["duration_ms"])),
			dim("…"))
		if preview == "" {
			return head, true
		}
		// Preview on its own indented line.
		return head + "\n" + strings.Repeat(" ", 22) + dim(preview), true
	case observability.EventLLMRequestFailed:
		data := mapFromAny(e.Data)
		return fmt.Sprintf("%s  %s %v", ts, red("LLM Failed"), data["error"]), true
	case observability.EventToolStarted:
		data := mapFromAny(e.Data)
		tool, _ := data["tool"].(string)
		args, _ := data["arguments"].(string)
		return fmt.Sprintf("%s  %s %s %s", ts, cyan("Tool Start"), bold(tool), gray(truncateStr(compactJSON(args), 50))), true
	case observability.EventToolFinished:
		data := mapFromAny(e.Data)
		tool, _ := data["tool"].(string)
		return fmt.Sprintf("%s  %s %s %s %s",
			ts, green("Tool Done"), bold(tool),
			gray(fmt.Sprintf("%v bytes", data["result_size"])),
			gray(fmt.Sprintf("%vms", data["duration_ms"]))), true
	case observability.EventToolFailed:
		data := mapFromAny(e.Data)
		tool, _ := data["tool"].(string)
		return fmt.Sprintf("%s  %s %s %v", ts, red("Tool Failed"), bold(tool), data["error"]), true
	case observability.EventLoopDetected:
		data := mapFromAny(e.Data)
		return fmt.Sprintf("%s  %s %v x%v", ts, red("Loop Detected"), data["tool"], data["count"]), true
	}
	return "", false
}

func paintState(state string) string {
	switch state {
	case "FINISHED":
		return green(state)
	case "FAILED", "CANCELLED", "MAX_STEPS_REACHED":
		return red(state)
	case "EXECUTING_TOOL", "CALLING_LLM":
		return yellow(state)
	case "BUILDING_CONTEXT", "PROCESSING_RESPONSE":
		return cyan(state)
	default:
		return state
	}
}

func mapFromAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func truncateStr(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	end := n
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "..."
}

func (a *App) printTrace(n int) {
	events, err := observability.ReadEvents(a.recorder.Path())
	if err != nil {
		fmt.Fprintf(a.out, "%s %v\n", red("read trace:"), err)
		return
	}
	fmt.Fprintf(a.out, "\n%s %s  %s\n\n", bold("Trace"), gray(a.recorder.Path()),
		gray(fmt.Sprintf("(%d events, showing last %d)", len(events), n)))
	start := 0
	if len(events) > n {
		start = len(events) - n
	}
	for i, e := range events[start:] {
		seq := start + i + 1
		fmt.Fprintf(a.out, "%s  %s\n", gray(fmt.Sprintf("%3d", seq)), formatEvent(e))
	}
	fmt.Fprintln(a.out)
}

func formatEvent(e observability.Event) string {
	ts := gray(e.Time.Local().Format("15:04:05.000"))
	typ := paintEventType(string(e.Type))
	base := fmt.Sprintf("%s  %s", ts, typ)

	data := mapFromAny(e.Data)
	if len(data) == 0 && e.Data != nil {
		b, _ := json.Marshal(e.Data)
		_ = json.Unmarshal(b, &data)
	}

	switch e.Type {
	case observability.EventLLMRequestFinished:
		return fmt.Sprintf("%s  in=%v out=%v total=%v  %vms",
			base, data["input_tokens"], data["output_tokens"], data["total_tokens"], data["duration_ms"])
	case observability.EventLLMRequestStarted:
		return fmt.Sprintf("%s  model=%v messages=%v", base, data["model"], data["message_count"])
	case observability.EventLLMRequestFailed:
		return fmt.Sprintf("%s  %v", base, red(fmt.Sprint(data["error"])))
	case observability.EventSessionCreated:
		return fmt.Sprintf("%s  model=%v provider=%v", base, data["model"], data["provider"])
	case observability.EventAgentStateChanged:
		to, _ := data["to"].(string)
		return fmt.Sprintf("%s  %v → %s", base, data["from"], paintState(to))
	case observability.EventToolStarted, observability.EventToolRequested:
		return fmt.Sprintf("%s  %v %v", base, data["tool"], gray(truncateStr(fmt.Sprint(data["arguments"]), 60)))
	case observability.EventToolFinished:
		errPart := green("ok")
		if v, ok := data["is_error"].(bool); ok && v {
			errPart = red("error")
		}
		return fmt.Sprintf("%s  %v  %v bytes  %vms %s", base, data["tool"], data["result_size"], data["duration_ms"], errPart)
	case observability.EventToolFailed:
		return fmt.Sprintf("%s  %v  %v", base, data["tool"], red(fmt.Sprint(data["error"])))
	}
	return base
}

func paintEventType(typ string) string {
	switch {
	case typ == "session.created":
		return blue(typ)
	case strings.HasPrefix(typ, "agent.finished"):
		return green(typ)
	case strings.HasPrefix(typ, "agent.failed") || strings.HasPrefix(typ, "llm.request_failed") || strings.HasPrefix(typ, "tool.failed") || typ == "loop.detected":
		return red(typ)
	case strings.HasPrefix(typ, "llm."):
		return magenta(typ)
	case strings.HasPrefix(typ, "tool."):
		return cyan(typ)
	case strings.HasPrefix(typ, "context."):
		return yellow(typ)
	case strings.HasPrefix(typ, "agent."):
		return bold(typ)
	default:
		return typ
	}
}

func atoi(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}
