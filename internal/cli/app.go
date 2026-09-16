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
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mincode/mincode/internal/agent"
	"github.com/mincode/mincode/internal/config"
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
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	if opts.Model != "" {
		cfg.Provider.Model = opts.Model
	}
	if opts.Provider != "" {
		cfg.Provider.Type = opts.Provider
	}

	workspace := opts.Workspace
	if workspace == "" {
		workspace, err = os.Getwd()
		if err != nil {
			workspace = "."
		}
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

	ag := agent.New(provider, registry, bus, sessionID, cfg.Agent.MaxSteps, cfg.Agent.SystemPrompt)

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
	fmt.Fprintf(a.out, "Min Code Agent  session=%s\n", a.sessionID)
	fmt.Fprintf(a.out, "provider=%s  model=%s  workspace=%s\n", a.provider.Name(), a.provider.Model(), a.workspace)
	fmt.Fprintf(a.out, "tools=%s\n", strings.Join(a.toolNames(), ", "))
	fmt.Fprintf(a.out, "trace=%s\n", a.recorder.Path())
	fmt.Fprintf(a.out, "Type a message, or /help for commands.\n\n")

	a.emit(observability.EventAgentStarted, observability.AgentLifecycleData{Reason: "repl"})

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for {
		fmt.Fprint(a.out, "mincode> ")
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
			fmt.Fprintf(w, "  → %s %s\n", data.Tool, compactJSON(data.Arguments))
		}
	case observability.EventToolFinished, observability.EventToolFailed:
		if data, ok := toolData(e.Data); ok {
			status := "ok"
			if data.IsError || e.Type == observability.EventToolFailed {
				status = "error"
			}
			dur := time.Duration(data.DurationMS) * time.Millisecond
			fmt.Fprintf(w, "  ← %s %s  %d bytes  %s\n", data.Tool, status, data.ResultSize, dur.Round(time.Millisecond))
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
		return s[:120] + "..."
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", " "); err != nil {
		if len(s) > 120 {
			return s[:120] + "..."
		}
		return s
	}
	out := strings.ReplaceAll(buf.String(), "\n", " ")
	if len(out) > 120 {
		return out[:120] + "..."
	}
	return out
}

func (a *App) handleCommand(line string) (quit bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	name := fields[0]

	switch name {
	case "/help", "/h", "/?":
		fmt.Fprint(a.out, `Commands:
  /help              show this help
  /timeline          show agent execution timeline
  /trace [n]         show last n raw trace events (default 30)
  /metrics           show session token/time metrics
  /clear             clear conversation history
  /exit, /quit       leave the REPL

Trace file:
`)
		fmt.Fprintf(a.out, "  %s\n", a.recorder.Path())
	case "/timeline":
		a.printTimeline()
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
		fmt.Fprint(a.out, a.metrics.Snapshot().Format())
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

func (a *App) printTimeline() {
	events, err := observability.ReadEvents(a.recorder.Path())
	if err != nil {
		fmt.Fprintf(a.out, "read trace: %v\n", err)
		return
	}
	fmt.Fprintf(a.out, "\nTimeline  %s\n\n", a.recorder.Path())
	seq := 0
	for _, e := range events {
		// Skip pure state chatter noise? Keep state but compact.
		line, ok := timelineLine(e)
		if !ok {
			continue
		}
		seq++
		fmt.Fprintf(a.out, "%3d  %s\n", seq, line)
	}
	fmt.Fprintln(a.out)
}

func timelineLine(e observability.Event) (string, bool) {
	ts := e.Time.Format("15:04:05.000")
	switch e.Type {
	case observability.EventSessionCreated:
		return fmt.Sprintf("%s  Session Created", ts), true
	case observability.EventAgentStarted:
		return fmt.Sprintf("%s  Agent Started", ts), true
	case observability.EventAgentFinished:
		return fmt.Sprintf("%s  Agent Finished", ts), true
	case observability.EventAgentFailed:
		return fmt.Sprintf("%s  Agent Failed", ts), true
	case observability.EventAgentStateChanged:
		data := mapFromAny(e.Data)
		to, _ := data["to"].(string)
		// Only surface meaningful operational states.
		switch to {
		case "BUILDING_CONTEXT", "CALLING_LLM", "PROCESSING_RESPONSE", "EXECUTING_TOOL",
			"FINISHED", "FAILED", "CANCELLED", "MAX_STEPS_REACHED":
			return fmt.Sprintf("%s  State → %s", ts, to), true
		}
		return "", false
	case observability.EventLLMRequestStarted:
		data := mapFromAny(e.Data)
		return fmt.Sprintf("%s  LLM Request   model=%v messages=%v", ts, data["model"], data["message_count"]), true
	case observability.EventLLMRequestFinished:
		data := mapFromAny(e.Data)
		preview, _ := data["content_preview"].(string)
		if len(preview) > 60 {
			preview = preview[:60] + "..."
		}
		return fmt.Sprintf("%s  LLM Response  in=%v out=%v  %vms  %s",
			ts, data["input_tokens"], data["output_tokens"], data["duration_ms"], preview), true
	case observability.EventLLMRequestFailed:
		data := mapFromAny(e.Data)
		return fmt.Sprintf("%s  LLM Failed    %v", ts, data["error"]), true
	case observability.EventToolStarted:
		data := mapFromAny(e.Data)
		tool, _ := data["tool"].(string)
		args, _ := data["arguments"].(string)
		return fmt.Sprintf("%s  Tool Start    %s %s", ts, tool, truncateStr(compactJSON(args), 50)), true
	case observability.EventToolFinished:
		data := mapFromAny(e.Data)
		tool, _ := data["tool"].(string)
		return fmt.Sprintf("%s  Tool Done     %s  %v bytes  %vms", ts, tool, data["result_size"], data["duration_ms"]), true
	case observability.EventToolFailed:
		data := mapFromAny(e.Data)
		tool, _ := data["tool"].(string)
		return fmt.Sprintf("%s  Tool Failed   %s  %v", ts, tool, data["error"]), true
	case observability.EventLoopDetected:
		data := mapFromAny(e.Data)
		return fmt.Sprintf("%s  Loop Detected %v x%v", ts, data["tool"], data["count"]), true
	}
	return "", false
}

func mapFromAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (a *App) printTrace(n int) {
	events, err := observability.ReadEvents(a.recorder.Path())
	if err != nil {
		fmt.Fprintf(a.out, "read trace: %v\n", err)
		return
	}
	fmt.Fprintf(a.out, "\nTrace %s  (%d events, showing last %d)\n\n", a.recorder.Path(), len(events), n)
	start := 0
	if len(events) > n {
		start = len(events) - n
	}
	for i, e := range events[start:] {
		seq := start + i + 1
		fmt.Fprintf(a.out, "%3d  %s\n", seq, formatEvent(e))
	}
	fmt.Fprintln(a.out)
}

func formatEvent(e observability.Event) string {
	ts := e.Time.Format("15:04:05.000")
	base := fmt.Sprintf("%s  %-22s", ts, e.Type)

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
		return fmt.Sprintf("%s  err=%v", base, data["error"])
	case observability.EventSessionCreated:
		return fmt.Sprintf("%s  model=%v provider=%v", base, data["model"], data["provider"])
	case observability.EventAgentStateChanged:
		return fmt.Sprintf("%s  %v → %v", base, data["from"], data["to"])
	case observability.EventToolStarted, observability.EventToolRequested:
		return fmt.Sprintf("%s  %v %v", base, data["tool"], truncateStr(fmt.Sprint(data["arguments"]), 60))
	case observability.EventToolFinished:
		return fmt.Sprintf("%s  %v  %v bytes  %vms err=%v", base, data["tool"], data["result_size"], data["duration_ms"], data["is_error"])
	case observability.EventToolFailed:
		return fmt.Sprintf("%s  %v  %v", base, data["tool"], data["error"])
	}
	return base
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
