package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Provider is the replaceable LLM backend interface.
type Provider interface {
	// Name returns a short provider identifier (e.g. "openai-compatible").
	Name() string
	// Model returns the configured model id.
	Model() string
	// Chat sends a request and returns a non-streaming response.
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

// ChatRequest is a provider-agnostic chat completion request.
type ChatRequest struct {
	Messages    []Message
	Tools       []ToolDefinition
	Temperature *float64
	MaxTokens   *int
	Model       string
}

// ChatResponse is a provider-agnostic chat completion response.
type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
	Usage     Usage
	// Raw is the original provider payload for debugging/export.
	Raw json.RawMessage
}

// Sentinel errors for provider failures.
var (
	ErrEmptyResponse = errors.New("llm: empty response")
	ErrNoAPIKey      = errors.New("llm: api key not configured")
)

// ProviderError wraps a provider-side failure with context.
type ProviderError struct {
	Provider string
	Status   int
	Body     string
	Err      error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s provider error (status %d): %v", e.Provider, e.Status, e.Err)
	}
	return fmt.Sprintf("%s provider error (status %d): %s", e.Provider, e.Status, truncate(e.Body, 200))
}

func (e *ProviderError) Unwrap() error { return e.Err }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
