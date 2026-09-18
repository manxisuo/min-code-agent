package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manxisuo/mincode/internal/agent"
	"github.com/manxisuo/mincode/internal/llm"
	"github.com/manxisuo/mincode/internal/observability"
	"github.com/manxisuo/mincode/internal/tools"
)

func testAgent(t *testing.T) (*agent.Agent, *observability.Bus, *observability.MetricsCollector) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello-web"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	reg.Register(&tools.ReadFile{WS: ws})

	bus := observability.NewBus()
	metrics := observability.NewMetricsCollector()
	bus.Subscribe(metrics.Handle)

	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{
				ToolCalls: []llm.ToolCall{
					{ID: "c1", Name: "read_file", Arguments: `{"path":"hello.txt"}`},
				},
			},
			{Content: "web final answer"},
		},
	}
	ag := agent.New(fake, reg, bus, "web-test", 5, "sys", 8000)
	return ag, bus, metrics
}

func TestWebAPIChatAndEvents(t *testing.T) {
	ag, bus, metrics := testAgent(t)
	srv := New(Options{
		Addr:      "127.0.0.1:0",
		Workspace: t.TempDir(),
		SessionID: "web-test",
		Provider:  "fake",
		Model:     "fake-model",
	}, ag, bus, metrics, nil, nil, nil)

	// Trace history API
	tr := httptest.NewServer(srv.Handler())
	defer tr.Close()
	res2, err := http.Get(tr.URL + "/api/traces")
	if err != nil {
		t.Fatal(err)
	}
	var tl map[string]any
	_ = json.NewDecoder(res2.Body).Decode(&tl)
	res2.Body.Close()
	if _, ok := tl["traces"]; !ok {
		t.Fatalf("traces list = %+v", tl)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Static UI
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("index status = %d", res.StatusCode)
	}
	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(res.Body)
	if !strings.Contains(body.String(), "MinCode Inspector") {
		t.Fatalf("index.html missing title")
	}

	// Start SSE in background
	sseCtx, cancelSSE := context.WithCancel(context.Background())
	defer cancelSSE()
	events := make(chan map[string]any, 64)
	go func() {
		req, _ := http.NewRequestWithContext(sseCtx, http.MethodGet, ts.URL+"/api/events", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		var dataLine string
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data: ") {
				dataLine = strings.TrimPrefix(line, "data: ")
			} else if line == "" && dataLine != "" {
				var m map[string]any
				if json.Unmarshal([]byte(dataLine), &m) == nil {
					select {
					case events <- m:
					default:
					}
				}
				dataLine = ""
			}
		}
	}()

	// Give SSE a moment to connect
	time.Sleep(50 * time.Millisecond)

	payload, _ := json.Marshal(map[string]string{"message": "read hello"})
	resp, err := http.Post(ts.URL+"/api/chat", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("chat status = %d", resp.StatusCode)
	}

	// Wait for agent to finish
	deadline := time.After(5 * time.Second)
	var finished bool
	sawTool := false
	for !finished {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for agent finish")
		case e := <-events:
			typ, _ := e["type"].(string)
			if typ == "tool.started" || typ == "tool.finished" {
				sawTool = true
			}
			if typ == "agent.state_changed" {
				if data, ok := e["data"].(map[string]any); ok {
					if data["to"] == "FINISHED" {
						finished = true
					}
				}
			}
		case <-time.After(200 * time.Millisecond):
			// also poll session endpoint
			sr, err := http.Get(ts.URL + "/api/session")
			if err == nil {
				var s map[string]any
				_ = json.NewDecoder(sr.Body).Decode(&s)
				sr.Body.Close()
				if running, ok := s["running"].(bool); ok && !running {
					if turn, ok := s["turn"].(map[string]any); ok {
						if final, _ := turn["final"].(string); strings.Contains(final, "web final answer") {
							finished = true
						}
					}
				}
			}
		}
	}

	mr, err := http.Get(ts.URL + "/api/metrics")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.NewDecoder(mr.Body).Decode(&m)
	mr.Body.Close()
	if m["llm_calls"] == nil {
		t.Fatalf("metrics = %+v", m)
	}

	cr, err := http.Get(ts.URL + "/api/context")
	if err != nil {
		t.Fatal(err)
	}
	var snap map[string]any
	_ = json.NewDecoder(cr.Body).Decode(&snap)
	cr.Body.Close()
	if snap["total_tokens"] == nil {
		t.Fatalf("context snapshot = %+v", snap)
	}

	_ = sawTool
}

func TestWebChatConflictWhileRunning(t *testing.T) {
	ag, bus, metrics := testAgent(t)
	// Slow the tool via a blocking provider? Use fake that waits on channel.
	block := make(chan struct{})
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{{Content: "slow"}},
		OnChat: func(req llm.ChatRequest) {
			<-block
		},
	}
	ag.Provider = fake

	srv := New(Options{SessionID: "c"}, ag, bus, metrics, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	payload, _ := json.Marshal(map[string]string{"message": "one"})
	resp, err := http.Post(ts.URL+"/api/chat", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp2, err := http.Post(ts.URL+"/api/chat", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("second chat status = %d, want 409", resp2.StatusCode)
	}
	close(block)
}
