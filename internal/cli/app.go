package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mincode/mincode/internal/config"
	"github.com/mincode/mincode/internal/llm"
	"github.com/mincode/mincode/internal/observability"
)

// Options are runtime options from flags.
type Options struct {
	ConfigPath string
	Prompt     string // single-shot mode when non-empty
	Model      string // override
	Provider   string // override
	Workspace  string
}

// App wires config, provider, observability and the REPL.
type App struct {
	cfg       config.Config
	provider  llm.Provider
	bus       *observability.Bus
	recorder  *observability.Recorder
	metrics   *observability.MetricsCollector
	sessionID string
	workspace string
	// history is the in-memory conversation for Phase 1 (no Context Manager yet).
	history []llm.Message
	out     io.Writer
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

	app := &App{
		cfg:       cfg,
		provider:  provider,
		bus:       bus,
		recorder:  recorder,
		metrics:   metrics,
		sessionID: sessionID,
		workspace: workspace,
		out:       os.Stdout,
		history: []llm.Message{
			{Role: llm.RoleSystem, Content: cfg.Agent.SystemPrompt},
		},
	}

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
			"This is a scripted Fake Provider response (Phase 1 offline mode).",
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
	a.bus.Publish(observability.NewEvent(a.sessionID, len(a.history), typ, data))
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
	resp, err := a.chat(ctx, prompt)
	if err != nil {
		a.emit(observability.EventAgentFailed, observability.AgentLifecycleData{Reason: err.Error()})
		return err
	}
	fmt.Fprintln(a.out, resp.Content)
	a.emit(observability.EventAgentFinished, observability.AgentLifecycleData{Reason: "single-shot complete"})
	return nil
}

func (a *App) repl(ctx context.Context) error {
	fmt.Fprintf(a.out, "Min Code Agent  session=%s\n", a.sessionID)
	fmt.Fprintf(a.out, "provider=%s  model=%s  trace=%s\n", a.provider.Name(), a.provider.Model(), a.recorder.Path())
	fmt.Fprintf(a.out, "Type a message, or /help for commands.\n\n")

	a.emit(observability.EventAgentStarted, observability.AgentLifecycleData{Reason: "repl"})

	in := bufio.NewScanner(os.Stdin)
	// Allow long pasted prompts.
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

		resp, err := a.chat(ctx, line)
		if err != nil {
			fmt.Fprintf(a.out, "error: %v\n", err)
			continue
		}
		fmt.Fprintln(a.out)
		fmt.Fprintln(a.out, resp.Content)
		fmt.Fprintln(a.out)
	}

	if err := in.Err(); err != nil {
		return err
	}
	a.emit(observability.EventAgentFinished, observability.AgentLifecycleData{Reason: "repl exit"})
	return nil
}

// chat sends one user turn through the provider and records traces/metrics.
func (a *App) chat(ctx context.Context, userText string) (*llm.ChatResponse, error) {
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: userText})

	start := time.Now()
	a.emit(observability.EventLLMRequestStarted, observability.LLMRequestData{
		Provider:     a.provider.Name(),
		Model:        a.provider.Model(),
		MessageCount: len(a.history),
	})

	resp, err := a.provider.Chat(ctx, llm.ChatRequest{
		Messages: a.history,
	})
	elapsed := time.Since(start)

	if err != nil {
		a.emit(observability.EventLLMRequestFailed, observability.LLMRequestData{
			Provider:   a.provider.Name(),
			Model:      a.provider.Model(),
			DurationMS: elapsed.Milliseconds(),
			Error:      err.Error(),
		})
		// Drop the failed user message so a retry does not duplicate it.
		a.history = a.history[:len(a.history)-1]
		return nil, err
	}

	preview := resp.Content
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	a.emit(observability.EventLLMRequestFinished, observability.LLMRequestData{
		Provider:       a.provider.Name(),
		Model:          a.provider.Model(),
		MessageCount:   len(a.history),
		InputTokens:    resp.Usage.PromptTokens,
		OutputTokens:   resp.Usage.CompletionTokens,
		TotalTokens:    resp.Usage.TotalTokens,
		DurationMS:     elapsed.Milliseconds(),
		ContentPreview: preview,
	})

	a.history = append(a.history, llm.Message{
		Role:      llm.RoleAssistant,
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	})
	return resp, nil
}

func (a *App) handleCommand(line string) (quit bool) {
	cmd := strings.TrimSpace(line)
	fields := strings.Fields(cmd)
	name := fields[0]

	switch name {
	case "/help", "/h", "/?":
		fmt.Fprint(a.out, `Commands:
  /help              show this help
  /trace [n]         show last n trace events (default 20)
  /metrics           show session token/time metrics
  /clear             clear in-memory conversation history
  /exit, /quit       leave the REPL

Trace file:
`)
		fmt.Fprintf(a.out, "  %s\n", a.recorder.Path())
	case "/trace":
		n := 20
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
		a.history = a.history[:1] // keep system prompt
		fmt.Fprintln(a.out, "history cleared (system prompt kept)")
	case "/exit", "/quit":
		return true
	default:
		fmt.Fprintf(a.out, "unknown command %s (try /help)\n", name)
	}
	return false
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
		line := formatEvent(e)
		fmt.Fprintf(a.out, "%3d  %s\n", seq, line)
	}
	fmt.Fprintln(a.out)
}

func formatEvent(e observability.Event) string {
	ts := e.Time.Format("15:04:05.000")
	base := fmt.Sprintf("%s  %-22s", ts, e.Type)

	data, ok := e.Data.(map[string]any)
	if !ok && e.Data != nil {
		// In-process path uses typed structs; re-marshal for uniform display.
		b, _ := jsonMarshal(e.Data)
		_ = jsonUnmarshal(b, &data)
	}
	if !ok || data == nil {
		return base
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

// tiny helpers to avoid importing encoding/json in multiple forms here
func jsonMarshal(v any) ([]byte, error) {
	return jsonMarshalImpl(v)
}

func jsonUnmarshal(b []byte, v any) error {
	return jsonUnmarshalImpl(b, v)
}

// DefaultWorkspace is exported for tests.
func DefaultWorkspace() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		return wd
	}
	return abs
}
