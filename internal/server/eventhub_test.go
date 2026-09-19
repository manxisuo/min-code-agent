package server

import (
	"strings"
	"testing"
	"time"

	"github.com/manxisuo/mincode/internal/observability"
)

func TestEventHubStreamDeltasBeforeRequestFinished(t *testing.T) {
	h := newEventHub()
	ch := h.subscribe()
	defer h.unsubscribe(ch)

	sid := "sess-hub"
	const n = 20
	// Simulate a burst of deltas then an immediate control event — the race
	// that used to let llm.request_finished overtake the stream tail.
	for i := 0; i < n; i++ {
		h.publish(observability.NewEvent(sid, 1, observability.EventLLMStreamDelta,
			observability.StreamDeltaData{Text: "x", Index: i + 1}))
	}
	h.publish(observability.NewEvent(sid, 1, observability.EventLLMRequestFinished,
		observability.LLMRequestData{Provider: "fake", Model: "m"}))

	var deltaText strings.Builder
	deadline := time.After(2 * time.Second)
	sawFinished := false
	for deltaText.Len() < n || !sawFinished {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatal("channel closed early")
			}
			switch e.Type {
			case observability.EventLLMStreamDelta:
				if sawFinished {
					t.Fatalf("delta arrived after request_finished (got %q)", deltaText.String())
				}
				d, _ := e.Data.(observability.StreamDeltaData)
				deltaText.WriteString(d.Text)
			case observability.EventLLMRequestFinished:
				sawFinished = true
			}
		case <-deadline:
			t.Fatalf("timeout: deltas=%q finished=%v", deltaText.String(), sawFinished)
		}
	}
	if got := deltaText.String(); got != strings.Repeat("x", n) {
		t.Fatalf("delta text = %q, want %d x's", got, n)
	}
}

func TestEventHubCoalescesDeltas(t *testing.T) {
	h := newEventHub()
	ch := h.subscribe()
	defer h.unsubscribe(ch)

	h.publish(observability.NewEvent("s", 1, observability.EventLLMStreamDelta,
		observability.StreamDeltaData{Text: "hel"}))
	h.publish(observability.NewEvent("s", 1, observability.EventLLMStreamDelta,
		observability.StreamDeltaData{Text: "lo"}))

	select {
	case e := <-ch:
		if e.Type != observability.EventLLMStreamDelta {
			t.Fatalf("type = %s", e.Type)
		}
		d := e.Data.(observability.StreamDeltaData)
		if d.Text != "hello" {
			t.Fatalf("coalesced text = %q, want hello", d.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for coalesced delta")
	}
}
