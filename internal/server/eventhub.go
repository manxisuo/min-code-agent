package server

import (
	"strings"
	"sync"
	"time"

	"github.com/manxisuo/mincode/internal/observability"
)

// hubFlushType is an internal tick that forces a pending delta flush on the
// dispatcher goroutine. It is never forwarded to subscribers.
const hubFlushType observability.EventType = "__hub_flush__"

// eventHub fans bus events out to SSE subscribers.
//
// Ordering guarantee: coalesced stream deltas are always delivered before any
// later non-delta event (llm.request_finished, agent.state_changed, …).
// All publishes go through a single dispatcher goroutine so a control event
// cannot overtake the tail of a stream.
type eventHub struct {
	in   chan observability.Event
	mu   sync.Mutex
	subs map[chan observability.Event]struct{}
	buf  []observability.Event
	// maxKeep bounds recent-event replay buffer.
	maxKeep int
}

func newEventHub() *eventHub {
	h := &eventHub{
		in:      make(chan observability.Event, 256),
		subs:    make(map[chan observability.Event]struct{}),
		maxKeep: 200,
	}
	go h.loop()
	return h
}

func (h *eventHub) publish(e observability.Event) {
	// Never block the agent on a slow SSE client; drop only if the dispatch
	// queue is saturated (local single-user UI, rare).
	select {
	case h.in <- e:
	default:
	}
}

func (h *eventHub) loop() {
	var (
		pending    []string
		pendLast   observability.Event
		flushTimer *time.Timer
	)

	emit := func(e observability.Event) {
		if e.Type == hubFlushType {
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

	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		merged := strings.Join(pending, "")
		parts := len(pending)
		base := pendLast
		pending = nil
		if merged == "" {
			return
		}
		emit(observability.Event{
			ID:        base.ID + "-batch",
			Time:      time.Now().UTC(),
			SessionID: base.SessionID,
			Step:      base.Step,
			Type:      observability.EventLLMStreamDelta,
			Data: observability.StreamDeltaData{
				Text:     merged,
				Index:    parts,
				TotalLen: len(merged),
			},
		})
	}

	stopTimer := func() {
		if flushTimer != nil {
			flushTimer.Stop()
			flushTimer = nil
		}
	}

	for e := range h.in {
		if e.Type == hubFlushType {
			flushTimer = nil
			flushPending()
			continue
		}
		if e.Type == observability.EventLLMStreamDelta {
			text := ""
			if d, ok := e.Data.(observability.StreamDeltaData); ok {
				text = d.Text
			}
			if text == "" {
				continue
			}
			pending = append(pending, text)
			pendLast = e
			if flushTimer == nil {
				flushTimer = time.AfterFunc(80*time.Millisecond, func() {
					select {
					case h.in <- observability.Event{Type: hubFlushType}:
					default:
					}
				})
			}
			continue
		}
		// Control/lifecycle event: flush coalesced deltas first so the
		// browser never sees request_finished before the stream tail.
		flushPending()
		stopTimer()
		emit(e)
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
