package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/manxisuo/mincode/internal/llm"
	"github.com/manxisuo/mincode/internal/observability"
	"github.com/manxisuo/mincode/internal/tools"
)

func TestAgentStreamingDeltas(t *testing.T) {
	fake := llm.NewFakeProvider("m", "streamed answer here")
	fake.StreamChunkSize = 4
	ws := testWorkspace(t)
	reg := tools.NewRegistry()
	reg.Register(&tools.ReadFile{WS: ws})
	bus := observability.NewBus()
	var deltas []string
	var types []observability.EventType
	bus.Subscribe(func(e observability.Event) {
		types = append(types, e.Type)
		if e.Type == observability.EventLLMStreamDelta {
			if d, ok := e.Data.(observability.StreamDeltaData); ok {
				deltas = append(deltas, d.Text)
			}
		}
	})

	ag := New(fake, reg, bus, "stream-s", 5, "sys", 8000)
	var collected strings.Builder
	bus.Subscribe(func(e observability.Event) {
		if e.Type == observability.EventLLMStreamDelta {
			if d, ok := e.Data.(observability.StreamDeltaData); ok {
				collected.WriteString(d.Text)
			}
		}
	})

	res, err := ag.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != "streamed answer here" {
		t.Fatalf("final = %q", res.Final)
	}
	if collected.String() != res.Final {
		t.Fatalf("onDelta = %q", collected.String())
	}
	if len(deltas) < 2 {
		t.Fatalf("delta events = %v", deltas)
	}

	var hasStart, hasDelta, hasFinish, hasReqFinish bool
	for _, typ := range types {
		switch typ {
		case observability.EventLLMStreamStarted:
			hasStart = true
		case observability.EventLLMStreamDelta:
			hasDelta = true
		case observability.EventLLMStreamFinished:
			hasFinish = true
		case observability.EventLLMRequestFinished:
			hasReqFinish = true
		}
	}
	if !hasStart || !hasDelta || !hasFinish || !hasReqFinish {
		t.Fatalf("missing stream events: %v", types)
	}
}

func TestAgentStreamingDisabledUsesChat(t *testing.T) {
	fake := llm.NewFakeProvider("m", "plain")
	reg := tools.NewRegistry()
	bus := observability.NewBus()
	var types []observability.EventType
	bus.Subscribe(func(e observability.Event) { types = append(types, e.Type) })
	ag := New(fake, reg, bus, "s", 5, "sys", 8000)
	ag.Stream = false
	if _, err := ag.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	for _, typ := range types {
		if typ == observability.EventLLMStreamDelta || typ == observability.EventLLMStreamStarted {
			t.Fatalf("stream events with Stream=false: %v", types)
		}
	}
}
