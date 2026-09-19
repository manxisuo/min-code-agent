package server

import (
	"strings"
	"sync"
	"time"

	"github.com/manxisuo/mincode/internal/observability"
)

// eventHub fans bus events out to SSE subscribers.
// Stream deltas are coalesced (~80ms) so a long answer does not flood
// the browser with hundreds of SSE frames and freeze the UI.
type eventHub struct {
	mu       sync.Mutex
	subs     map[chan observability.Event]struct{}
	buf      []observability.Event
	maxKeep  int
	pending  []string
	pendLast observability.Event
	flushing bool
}

func newEventHub() *eventHub {
	return &eventHub{
		subs:    make(map[chan observability.Event]struct{}),
		maxKeep: 200,
	}
}

func (h *eventHub) publish(e observability.Event) {
	if e.Type == observability.EventLLMStreamDelta {
		text := ""
		if d, ok := e.Data.(observability.StreamDeltaData); ok {
			text = d.Text
		}
		if text != "" {
			h.mu.Lock()
			h.pending = append(h.pending, text)
			h.pendLast = e
			need := !h.flushing
			if need {
				h.flushing = true
			}
			h.mu.Unlock()
			if need {
				go h.flushDeltasSoon()
			}
		}
		return
	}

	h.mu.Lock()
	h.buf = append(h.buf, e)
	if len(h.buf) > h.maxKeep {
		h.buf = h.buf[len(h.buf)-h.maxKeep:]
	}
	subs := make([]chan observability.Event, 0, len(h.subs))
	for ch := range h.subs {
		subs = append(subs, ch)
	}
	h.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (h *eventHub) flushDeltasSoon() {
	time.Sleep(80 * time.Millisecond)
	h.mu.Lock()
	parts := h.pending
	base := h.pendLast
	h.pending = nil
	h.flushing = false
	merged := strings.Join(parts, "")
	h.mu.Unlock()
	if merged == "" {
		return
	}
	ev := observability.Event{
		ID:        base.ID + "-batch",
		Time:      time.Now().UTC(),
		SessionID: base.SessionID,
		Step:      base.Step,
		Type:      observability.EventLLMStreamDelta,
		Data: observability.StreamDeltaData{
			Text:     merged,
			Index:    len(parts),
			TotalLen: len(merged),
		},
	}

	h.mu.Lock()
	h.buf = append(h.buf, ev)
	if len(h.buf) > h.maxKeep {
		h.buf = h.buf[len(h.buf)-h.maxKeep:]
	}
	subs := make([]chan observability.Event, 0, len(h.subs))
	for ch := range h.subs {
		subs = append(subs, ch)
	}
	h.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
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
