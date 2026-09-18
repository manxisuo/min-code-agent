package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/manxisuo/mincode/internal/agent"
	"github.com/manxisuo/mincode/internal/experiment"
	"github.com/manxisuo/mincode/internal/observability"
	webui "github.com/manxisuo/mincode/web"
)

// Options configures a local Web Inspector server (single user, single session).
type Options struct {
	Addr      string
	Workspace string
	SessionID string
	Provider  string
	Model     string
	// TraceDir is the primary directory for session JSONL traces.
	TraceDir string
	// TraceDirExtra is an optional legacy directory merged when listing/reading traces.
	TraceDirExtra string
}

// Server exposes Agent runtime over HTTP + SSE for the local Web UI.
type Server struct {
	opts        Options
	agent       *agent.Agent
	bus         *observability.Bus
	metrics     *observability.MetricsCollector
	experiments *experiment.Store
	traceDir    string

	hub *eventHub

	mu          sync.Mutex
	running     bool
	cancelTurn  context.CancelFunc
	lastResult  *agent.Result
	lastErr     string
	turnStarted time.Time
	// turnSeq increments on each /api/chat; completedTurn is the turn
	// whose result is in lastResult. Prevents stale finals from being
	// re-served as the "current" answer while a new turn runs.
	turnSeq       int
	completedTurn int
}

// New wires an agent + bus into an HTTP server. Subscribe on the bus so all
// runtime events fan out to SSE clients without touching the agent loop.
// exp may be nil when experiment APIs should report empty lists.
func New(opts Options, ag *agent.Agent, bus *observability.Bus, metrics *observability.MetricsCollector, exp *experiment.Store) *Server {
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:8080"
	}
	traceDir := opts.TraceDir
	if traceDir == "" && opts.Workspace != "" {
		traceDir = filepath.Join(opts.Workspace, ".mincode", "traces")
	}
	if traceDir == "" {
		traceDir = ".mincode/traces"
	}
	s := &Server{
		opts:        opts,
		agent:       ag,
		bus:         bus,
		metrics:     metrics,
		experiments: exp,
		traceDir:    traceDir,
		hub:         newEventHub(),
	}
	if bus != nil {
		bus.Subscribe(func(e observability.Event) {
			s.hub.publish(e)
		})
	}
	return s
}

// Handler returns the HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("POST /api/cancel", s.handleCancel)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/context", s.handleContext)
	mux.HandleFunc("GET /api/metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/timeline", s.handleTimeline)
	mux.HandleFunc("GET /api/traces", s.handleTraceList)
	mux.HandleFunc("GET /api/traces/{id}", s.handleTraceShow)
	mux.HandleFunc("GET /api/experiments", s.handleExperimentList)
	mux.HandleFunc("GET /api/experiments/{name}", s.handleExperimentShow)

	static, err := fs.Sub(webui.FS, "dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(static))
		mux.Handle("GET /", fileServer)
	}
	return mux
}

// ListenAndServe blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.opts.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSession(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	running := s.running
	state := string(s.agent.State)
	lastResult := s.lastResult
	lastErr := s.lastErr
	started := s.turnStarted
	completed := s.completedTurn
	s.mu.Unlock()

	var final string
	var steps, toolCalls int
	// Only expose the last answer when no turn is in flight.
	if !running && lastResult != nil {
		final = lastResult.Final
		steps = lastResult.Steps
		toolCalls = lastResult.ToolCalls
	}
	var snap any
	if lastResult != nil && lastResult.Snapshot != nil {
		snap = lastResult.Snapshot
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": s.opts.SessionID,
		"workspace":  s.opts.Workspace,
		"provider":   s.opts.Provider,
		"model":      s.opts.Model,
		"state":      state,
		"running":    running,
		"last_error": lastErr,
		"turn": map[string]any{
			"id":         completed,
			"final":      final,
			"steps":      steps,
			"tool_calls": toolCalls,
			"started_at": started,
		},
		"snapshot": snap,
	})
}

type chatRequest struct {
	Message string `json:"message"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	msg := req.Message
	if msg == "" {
		writeErr(w, http.StatusBadRequest, "message is required")
		return
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "a turn is already running")
		return
	}
	// Detach from the HTTP request: the turn continues after the response.
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelTurn = cancel
	s.running = true
	s.lastErr = ""
	s.turnStarted = time.Now().UTC()
	s.turnSeq++
	turn := s.turnSeq
	// Drop previous final so clients polling mid-turn do not re-append it.
	s.lastResult = nil
	s.mu.Unlock()

	go func() {
		defer cancel()
		res, err := s.agent.Run(ctx, msg)
		s.mu.Lock()
		s.running = false
		s.cancelTurn = nil
		s.lastResult = res
		s.completedTurn = turn
		switch {
		case err == nil:
			s.lastErr = ""
		case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
			s.lastErr = "cancelled by user"
		default:
			s.lastErr = err.Error()
		}
		s.mu.Unlock()
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "running": true})
}

func (s *Server) handleCancel(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	cancel := s.cancelTurn
	s.mu.Unlock()
	if cancel == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cancelled": false})
		return
	}
	cancel()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cancelled": true})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	// Hello event so the client knows the stream is live.
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", mustJSON(map[string]any{
		"session_id": s.opts.SessionID,
		"time":       time.Now().UTC(),
	}))
	flusher.Flush()

	ctx := r.Context()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				return
			}
			payload := map[string]any{
				"id":      e.ID,
				"time":    e.Time,
				"session": e.SessionID,
				"step":    e.Step,
				"type":    e.Type,
				"data":    e.Data,
			}
			fmt.Fprintf(w, "event: runtime\ndata: %s\n\n", mustJSON(payload))
			flusher.Flush()
		}
	}
}

func (s *Server) handleContext(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	res := s.lastResult
	s.mu.Unlock()
	if res == nil || res.Snapshot == nil {
		// Fall back to agent's last snapshot if any.
		if snap := s.agent.Ctx.LastSnapshot(); snap != nil {
			writeJSON(w, http.StatusOK, snap)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "total_tokens": 0})
		return
	}
	writeJSON(w, http.StatusOK, res.Snapshot)
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	m := observability.Metrics{}
	if s.metrics != nil {
		m = s.metrics.Snapshot()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"llm_calls":           m.LLMCalls,
		"errors":              m.Errors,
		"input_tokens":        m.InputTokens,
		"output_tokens":       m.OutputTokens,
		"total_tokens":        m.TotalTokens,
		"llm_duration_ms":     m.LLMDuration.Milliseconds(),
		"parallel_batches":    m.ParallelBatches,
		"parallel_tool_calls": m.ParallelToolCalls,
		"stream_calls":        m.StreamCalls,
		"stream_deltas":       m.StreamDeltas,
		"last_ttft_ms":        m.LastTTFTMS,
	})
}

// handleTimeline returns a recent in-memory buffer of runtime events.
func (s *Server) handleTimeline(w http.ResponseWriter, _ *http.Request) {
	events := s.hub.recent(200)
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"marshal"}`
	}
	return string(b)
}

// eventHub fans bus events out to SSE subscribers and keeps a short buffer.
type eventHub struct {
	mu      sync.Mutex
	subs    map[chan observability.Event]struct{}
	buf     []observability.Event
	maxKeep int
}

func newEventHub() *eventHub {
	return &eventHub{
		subs:    make(map[chan observability.Event]struct{}),
		maxKeep: 200,
	}
}

func (h *eventHub) publish(e observability.Event) {
	h.mu.Lock()
	h.buf = append(h.buf, e)
	if len(h.buf) > h.maxKeep {
		h.buf = h.buf[len(h.buf)-h.maxKeep:]
	}
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			// Slow client: drop event rather than block the agent.
		}
	}
	h.mu.Unlock()
}

func (h *eventHub) subscribe() chan observability.Event {
	ch := make(chan observability.Event, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *eventHub) unsubscribe(ch chan observability.Event) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

func (h *eventHub) recent(n int) []observability.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.buf
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	cp := make([]observability.Event, len(out))
	copy(cp, out)
	return cp
}
