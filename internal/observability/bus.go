package observability

import "sync"

// Handler receives published events.
type Handler func(Event)

// Bus is a simple in-process synchronous event bus.
// Handlers run on the publisher's goroutine in subscription order.
type Bus struct {
	mu       sync.RWMutex
	handlers []Handler
}

// NewBus creates an empty event bus.
func NewBus() *Bus {
	return &Bus{}
}

// Subscribe registers a handler for all events.
func (b *Bus) Subscribe(h Handler) {
	if h == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, h)
}

// Publish delivers the event to all subscribers.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	handlers := make([]Handler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.RUnlock()

	for _, h := range handlers {
		h(e)
	}
}
