package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFakeProviderScriptedOrder(t *testing.T) {
	f := NewFakeProvider("fake-model", "first", "second")
	r1, err := f.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Content != "first" {
		t.Fatalf("r1 = %q", r1.Content)
	}
	r2, err := f.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Content != "second" {
		t.Fatalf("r2 = %q", r2.Content)
	}
	// Last response repeats.
	r3, err := f.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if r3.Content != "second" {
		t.Fatalf("r3 = %q", r3.Content)
	}
	if f.Calls() != 3 {
		t.Fatalf("calls = %d", f.Calls())
	}
}

func TestFakeProviderError(t *testing.T) {
	f := NewFakeProvider("m", "x")
	f.Err = errors.New("forced")
	if _, err := f.Chat(context.Background(), ChatRequest{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestFakeProviderCancelled(t *testing.T) {
	f := NewFakeProvider("m", "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Chat(ctx, ChatRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompatibleProviderChat(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "hello from test"}},
			},
			"usage": map[string]any{
				"prompt_tokens":     12,
				"completion_tokens": 4,
				"total_tokens":      16,
			},
		})
	}))
	defer srv.Close()

	p := NewCompatibleProvider(srv.URL, "sk-abc", "test-model", 0.5, 256, 5)
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello from test" {
		t.Fatalf("content = %q", resp.Content)
	}
	if resp.Usage.TotalTokens != 16 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	if gotAuth != "Bearer sk-abc" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["model"] != "test-model" {
		t.Fatalf("model = %v", gotBody["model"])
	}
}

func TestCompatibleProviderHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	p := NewCompatibleProvider(srv.URL, "sk-bad", "m", 0, 0, 5)
	_, err := p.Chat(context.Background(), ChatRequest{})
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want ProviderError", err)
	}
	if pe.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d", pe.Status)
	}
}

func TestCompatibleProviderNoAPIKey(t *testing.T) {
	p := NewCompatibleProvider("http://localhost", "", "m", 0, 0, 5)
	if _, err := p.Chat(context.Background(), ChatRequest{}); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompatibleProviderEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	p := NewCompatibleProvider(srv.URL, "k", "m", 0, 0, 5)
	if _, err := p.Chat(context.Background(), ChatRequest{}); !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompatibleProviderToolCallsDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      "read_file",
									"arguments": `{"path":"a.go"}`,
								},
							},
						},
					},
				},
			},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer srv.Close()

	p := NewCompatibleProvider(srv.URL, "k", "m", 0, 0, 5)
	resp, err := p.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "read_file" {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
}

func TestCompatibleProviderSendsTools(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	defer srv.Close()

	p := NewCompatibleProvider(srv.URL, "k", "m", 0, 0, 5)
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []ToolDefinition{{
			Name:        "grep",
			Description: "search",
			Parameters:  []byte(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools, ok := got["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", got["tools"])
	}
}
