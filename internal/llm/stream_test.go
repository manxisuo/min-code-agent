package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFakeChatStreamChunks(t *testing.T) {
	f := NewFakeProvider("m", "hello streaming world")
	f.StreamChunkSize = 5
	var parts []string
	resp, err := f.ChatStream(context.Background(), ChatRequest{}, func(text string) {
		parts = append(parts, text)
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello streaming world" {
		t.Fatalf("content = %q", resp.Content)
	}
	if len(parts) < 2 {
		t.Fatalf("parts = %v", parts)
	}
	joined := strings.Join(parts, "")
	if joined != resp.Content {
		t.Fatalf("joined = %q", joined)
	}
}

func TestCompatibleChatStreamSSE(t *testing.T) {
	var written strings.Builder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing auth")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`data: {"choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"x\"}"}}]}}]}`,
			`data: {"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			written.WriteString(c)
			written.WriteByte('\n')
			_, _ = w.Write([]byte(c + "\n\n"))
		}
	}))
	defer srv.Close()

	p := NewCompatibleProvider(srv.URL, "key", "model-x", 0.1, 0, 5)
	var got []string
	resp, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []ToolDefinition{
			{Name: "read_file", Description: "r", Parameters: nil},
		},
	}, func(text string) {
		got = append(got, text)
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello" {
		t.Fatalf("content = %q\nwritten:\n%s", resp.Content, written.String())
	}
	if strings.Join(got, "") != "Hello" {
		t.Fatalf("deltas = %v", got)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v\nwritten:\n%s\nraw=%s", resp.ToolCalls, written.String(), string(resp.Raw))
	}
	tc := resp.ToolCalls[0]
	if tc.Name != "read_file" || tc.ID != "call_1" {
		t.Fatalf("tc = %+v", tc)
	}
	if tc.Arguments != `{"path":"x"}` {
		t.Fatalf("args = %q", tc.Arguments)
	}
	if resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestCompatibleChatStreamCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"))
		if fl != nil {
			fl.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	p := NewCompatibleProvider(srv.URL, "key", "m", 0.1, 0, 5)
	done := make(chan error, 1)
	go func() {
		_, err := p.ChatStream(ctx, ChatRequest{}, func(text string) {
			cancel()
		})
		done <- err
	}()
	err := <-done
	if err == nil {
		t.Fatal("expected cancel error")
	}
}
