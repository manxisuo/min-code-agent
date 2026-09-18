package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// StreamingProvider is an optional Provider that can deliver text deltas
// while generating. The returned ChatResponse is still the full aggregate.
type StreamingProvider interface {
	Provider
	// ChatStream calls onDelta with each content fragment (may be empty for
	// tool-call-only chunks). onDelta must be non-nil.
	ChatStream(ctx context.Context, req ChatRequest, onDelta func(text string)) (*ChatResponse, error)
}

// IsStreamingProvider reports whether p implements StreamingProvider.
func IsStreamingProvider(p Provider) (StreamingProvider, bool) {
	sp, ok := p.(StreamingProvider)
	return sp, ok
}

type wireStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// streamChoiceDelta is one OpenAI-compatible SSE delta payload.
type streamPayload struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type toolCallAgg struct {
	id   string
	name string
	args strings.Builder
}

// ChatStream implements StreamingProvider for OpenAI-compatible SSE APIs.
func (p *CompatibleProvider) ChatStream(ctx context.Context, req ChatRequest, onDelta func(text string)) (*ChatResponse, error) {
	if onDelta == nil {
		return p.Chat(ctx, req)
	}
	if p.APIKey == "" {
		return nil, ErrNoAPIKey
	}
	model := req.Model
	if model == "" {
		model = p.ModelName
	}

	wire := wireRequest{
		Model:    model,
		Messages: toWireMessages(req.Messages),
		Stream:   true,
		StreamOptions: &wireStreamOptions{
			IncludeUsage: true,
		},
	}
	if req.Temperature != nil {
		wire.Temperature = req.Temperature
	} else {
		t := p.Temperature
		wire.Temperature = &t
	}
	if req.MaxTokens != nil {
		wire.MaxTokens = req.MaxTokens
	} else if p.MaxTokens > 0 {
		mt := p.MaxTokens
		wire.MaxTokens = &mt
	}
	if len(req.Tools) > 0 {
		wire.Tools = toWireTools(req.Tools)
	}

	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	url := p.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.HTTPClient.Do(httpReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, &ProviderError{Provider: p.Name(), Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, &ProviderError{Provider: p.Name(), Status: resp.StatusCode, Body: string(raw)}
	}

	out := &ChatResponse{}
	var content strings.Builder
	toolAgg := map[int]*toolCallAgg{}
	var usage Usage
	var raw strings.Builder

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		raw.WriteString(data)
		raw.WriteByte('\n')

		var payload streamPayload
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			continue
		}
		if payload.Usage != nil {
			usage = Usage{
				PromptTokens:     payload.Usage.PromptTokens,
				CompletionTokens: payload.Usage.CompletionTokens,
				TotalTokens:      payload.Usage.TotalTokens,
			}
		}
		for _, ch := range payload.Choices {
			if text := ch.Delta.Content; text != "" {
				content.WriteString(text)
				onDelta(text)
			}
			for _, tc := range ch.Delta.ToolCalls {
				agg, ok := toolAgg[tc.Index]
				if !ok {
					agg = &toolCallAgg{}
					toolAgg[tc.Index] = agg
				}
				if tc.ID != "" {
					agg.id = tc.ID
				}
				if tc.Function.Name != "" {
					agg.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					agg.args.WriteString(tc.Function.Arguments)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, &ProviderError{Provider: p.Name(), Status: resp.StatusCode, Err: fmt.Errorf("read stream: %w", err)}
	}

	out.Content = content.String()
	out.Usage = usage
	if len(toolAgg) > 0 {
		// Preserve index order.
		maxIdx := 0
		for idx := range toolAgg {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		for i := 0; i <= maxIdx; i++ {
			agg, ok := toolAgg[i]
			if !ok || agg.name == "" {
				continue
			}
			id := agg.id
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			args := agg.args.String()
			if args == "" {
				args = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        id,
				Name:      agg.name,
				Arguments: args,
			})
		}
	}
	if out.Content == "" && len(out.ToolCalls) == 0 && usage.TotalTokens == 0 {
		// Empty stream — treat like empty non-stream response.
		if raw.Len() == 0 {
			return nil, ErrEmptyResponse
		}
	}
	out.Raw = json.RawMessage(fmt.Sprintf(`{"stream":true,"chunks":%q}`, raw.String()))
	if usage.PromptTokens == 0 && usage.TotalTokens == 0 {
		// Estimate when provider omitted usage on the stream.
		est := estimateTokens(out.Content)
		out.Usage = Usage{
			PromptTokens:     0,
			CompletionTokens: est,
			TotalTokens:      est,
		}
	}
	return out, nil
}
