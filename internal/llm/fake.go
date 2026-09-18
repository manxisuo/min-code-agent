package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// FakeProvider is a scripted provider for tests and offline demos.
type FakeProvider struct {
	mu sync.Mutex
	// Responses are returned in order; the last one repeats.
	Responses []ChatResponse
	// Err, if set, is returned on every Chat call.
	Err error
	// OnChat is an optional hook invoked with each request.
	OnChat func(req ChatRequest)
	// StreamChunkSize splits content when ChatStream is used (default 8 runes).
	StreamChunkSize int
	// OnStreamDelta is an optional hook for every streamed fragment.
	OnStreamDelta func(text string)

	model string
	calls int
	reqs  []ChatRequest
}

// NewFakeProvider creates a fake provider that returns the given contents in order.
func NewFakeProvider(model string, contents ...string) *FakeProvider {
	responses := make([]ChatResponse, 0, len(contents))
	for _, c := range contents {
		responses = append(responses, ChatResponse{
			Content: c,
			Usage: Usage{
				PromptTokens:     estimateTokens(c) + 8,
				CompletionTokens: estimateTokens(c),
				TotalTokens:      estimateTokens(c)*2 + 8,
			},
		})
	}
	return &FakeProvider{Responses: responses, model: model}
}

func (f *FakeProvider) Name() string { return "fake" }

func (f *FakeProvider) Model() string {
	if f.model != "" {
		return f.model
	}
	return "fake-model"
}

// Calls returns how many times Chat was invoked.
func (f *FakeProvider) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Requests returns a copy of all captured requests.
func (f *FakeProvider) Requests() []ChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ChatRequest, len(f.reqs))
	copy(out, f.reqs)
	return out
}

// Chat implements Provider.
func (f *FakeProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	f.mu.Lock()
	f.calls++
	f.reqs = append(f.reqs, req)
	err := f.Err
	var resp ChatResponse
	if len(f.Responses) > 0 {
		idx := f.calls - 1
		if idx >= len(f.Responses) {
			idx = len(f.Responses) - 1
		}
		resp = f.Responses[idx]
	} else {
		resp = ChatResponse{
			Content: "(fake provider has no scripted response)",
			Usage:   Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
		}
	}
	hook := f.OnChat
	f.mu.Unlock()

	if hook != nil {
		hook(req)
	}
	if err != nil {
		return nil, err
	}

	// Return a deep-ish copy so callers cannot mutate the script.
	out := resp
	out.Raw = json.RawMessage(fmt.Sprintf(`{"fake":true,"call":%d}`, f.calls))
	return &out, nil
}

// ChatStream implements StreamingProvider by replaying scripted content in chunks.
func (f *FakeProvider) ChatStream(ctx context.Context, req ChatRequest, onDelta func(text string)) (*ChatResponse, error) {
	if onDelta == nil {
		return f.Chat(ctx, req)
	}
	resp, err := f.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	size := f.StreamChunkSize
	if size <= 0 {
		size = 8
	}
	runes := []rune(resp.Content)
	hook := f.OnStreamDelta
	for i := 0; i < len(runes); i += size {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		piece := string(runes[i:end])
		onDelta(piece)
		if hook != nil {
			hook(piece)
		}
	}
	return resp, nil
}

// estimateTokens is a rough 4-chars-per-token heuristic for the fake provider.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	n := (len(s) + 3) / 4
	if n < 1 {
		return 1
	}
	return n
}
