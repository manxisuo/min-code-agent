package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mincode/mincode/internal/llm"
	"github.com/mincode/mincode/internal/observability"
	"github.com/mincode/mincode/internal/tools"
)

func testWorkspace(t *testing.T) *tools.Workspace {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func newAgentWithFake(t *testing.T, fake *llm.FakeProvider) (*Agent, *observability.Bus) {
	t.Helper()
	ws := testWorkspace(t)
	reg := tools.NewRegistry()
	reg.Register(&tools.ReadFile{WS: ws})
	reg.Register(&tools.ListDir{WS: ws})
	reg.Register(&tools.Glob{WS: ws})
	reg.Register(&tools.Grep{WS: ws})
	bus := observability.NewBus()
	var events []observability.Event
	bus.Subscribe(func(e observability.Event) { events = append(events, e) })
	ag := New(fake, reg, bus, "test-session", 10, "system")
	return ag, bus
}

func TestAgentFinalWithoutTools(t *testing.T) {
	fake := llm.NewFakeProvider("m", "final answer")
	ag, _ := newAgentWithFake(t, fake)

	res, err := ag.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != "final answer" {
		t.Fatalf("final = %q", res.Final)
	}
	if res.ToolCalls != 0 {
		t.Fatalf("tool calls = %d", res.ToolCalls)
	}
	if res.Steps != 1 {
		t.Fatalf("steps = %d", res.Steps)
	}
}

func TestAgentToolLoop(t *testing.T) {
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{
				ToolCalls: []llm.ToolCall{{
					ID:        "call_1",
					Name:      "read_file",
					Arguments: `{"path":"go.mod"}`,
				}},
			},
			{
				Content: "module example.com/demo",
				Usage:   llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		},
	}
	ag, _ := newAgentWithFake(t, fake)

	res, err := ag.Run(context.Background(), "read go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != "module example.com/demo" {
		t.Fatalf("final = %q", res.Final)
	}
	if res.ToolCalls != 1 {
		t.Fatalf("tool calls = %d", res.ToolCalls)
	}
	if res.Steps != 2 {
		t.Fatalf("steps = %d", res.Steps)
	}

	// Second LLM call must have received the tool result.
	reqs := fake.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	foundToolMsg := false
	for _, m := range reqs[1].Messages {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "module example.com/demo") {
			foundToolMsg = true
		}
	}
	if !foundToolMsg {
		t.Fatalf("tool result missing from second request: %+v", reqs[1].Messages)
	}
	// Tools should be advertised.
	if len(reqs[0].Tools) != 4 {
		t.Fatalf("advertised tools = %d", len(reqs[0].Tools))
	}
}

func TestAgentUnknownToolRecovered(t *testing.T) {
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{ID: "1", Name: "destroy_everything", Arguments: `{}`}}},
			{Content: "ok after error"},
		},
	}
	ag, _ := newAgentWithFake(t, fake)
	res, err := ag.Run(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != "ok after error" {
		t.Fatalf("final = %q", res.Final)
	}
}

func TestAgentLoopDetection(t *testing.T) {
	fake := &llm.FakeProvider{}
	// Always request the same tool call.
	fake.Responses = []llm.ChatResponse{{
		ToolCalls: []llm.ToolCall{{ID: "1", Name: "list_dir", Arguments: `{"path":"."}`}},
	}}
	ag, _ := newAgentWithFake(t, fake)
	ag.MaxSteps = 20

	_, err := ag.Run(context.Background(), "loop")
	if !errors.Is(err, LoopDetected) {
		t.Fatalf("err = %v, want LoopDetected", err)
	}
}

func TestAgentMaxSteps(t *testing.T) {
	prov := &alwaysToolProvider{}
	ag, _ := newAgentWithFake(t, llm.NewFakeProvider("m", "x"))
	ag.Provider = prov
	ag.MaxSteps = 3

	_, err := ag.Run(context.Background(), "go")
	if !errors.Is(err, MaxStepsExceeded) {
		t.Fatalf("err = %v, want MaxStepsExceeded", err)
	}
	if ag.State != StateMaxStepsReached {
		t.Fatalf("state = %s", ag.State)
	}
}

type alwaysToolProvider struct {
	n int
}

func (p *alwaysToolProvider) Name() string  { return "always" }
func (p *alwaysToolProvider) Model() string { return "m" }

func (p *alwaysToolProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	p.n++
	return &llm.ChatResponse{
		ToolCalls: []llm.ToolCall{{
			ID:        "c",
			Name:      "list_dir",
			Arguments: `{"path":"."}`,
		}},
	}, nil
}

func TestAgentCancelled(t *testing.T) {
	fake := llm.NewFakeProvider("m", "should not return")
	ag, _ := newAgentWithFake(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ag.Run(ctx, "hi")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestClearConversation(t *testing.T) {
	fake := llm.NewFakeProvider("m", "a", "b")
	ag, _ := newAgentWithFake(t, fake)
	if _, err := ag.Run(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := ag.Run(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	ag.ClearConversation()
	if len(ag.History) != 1 || ag.History[0].Role != llm.RoleSystem {
		t.Fatalf("history = %+v", ag.History)
	}
}
