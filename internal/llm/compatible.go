package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CompatibleProvider talks to any OpenAI-compatible /chat/completions API.
type CompatibleProvider struct {
	BaseURL     string
	APIKey      string
	ModelName   string
	Temperature float64
	MaxTokens   int
	HTTPClient  *http.Client
}

// NewCompatibleProvider builds a provider from config-like fields.
func NewCompatibleProvider(baseURL, apiKey, model string, temperature float64, maxTokens, timeoutSec int) *CompatibleProvider {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	return &CompatibleProvider{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		APIKey:      apiKey,
		ModelName:   model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		HTTPClient:  &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
	}
}

func (p *CompatibleProvider) Name() string { return "openai-compatible" }

func (p *CompatibleProvider) Model() string {
	if p.ModelName != "" {
		return p.ModelName
	}
	return "gpt-4o-mini"
}

// wireMessage is the OpenAI chat message wire format.
type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type wireRequest struct {
	Model         string             `json:"model"`
	Messages      []wireMessage      `json:"messages"`
	Temperature   *float64           `json:"temperature,omitempty"`
	MaxTokens     *int               `json:"max_tokens,omitempty"`
	Tools         []wireTool         `json:"tools,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StreamOptions *wireStreamOptions `json:"stream_options,omitempty"`
}

type wireResponse struct {
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat implements Provider.
func (p *CompatibleProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
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
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.HTTPClient.Do(httpReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, &ProviderError{Provider: p.Name(), Err: err}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, &ProviderError{Provider: p.Name(), Status: resp.StatusCode, Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &ProviderError{Provider: p.Name(), Status: resp.StatusCode, Body: string(raw)}
	}

	var wr wireResponse
	if err := json.Unmarshal(raw, &wr); err != nil {
		return nil, &ProviderError{Provider: p.Name(), Status: resp.StatusCode, Err: fmt.Errorf("decode response: %w", err)}
	}
	if len(wr.Choices) == 0 {
		return nil, ErrEmptyResponse
	}

	choice := wr.Choices[0]
	out := &ChatResponse{
		Content: choice.Message.Content,
		Usage: Usage{
			PromptTokens:     wr.Usage.PromptTokens,
			CompletionTokens: wr.Usage.CompletionTokens,
			TotalTokens:      wr.Usage.TotalTokens,
		},
		Raw: json.RawMessage(raw),
	}
	for _, tc := range choice.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out, nil
}

func toWireMessages(msgs []Message) []wireMessage {
	out := make([]wireMessage, 0, len(msgs))
	for _, m := range msgs {
		wm := wireMessage{
			Role:       string(m.Role),
			ToolCallID: m.ToolCallID,
		}
		// Always include content for assistant/tool (some providers require the key).
		if m.Content != "" || m.Role == RoleAssistant || m.Role == RoleTool {
			wm.Content = m.Content
		} else {
			wm.Content = m.Content
		}
		for _, tc := range m.ToolCalls {
			wtc := wireToolCall{ID: tc.ID, Type: "function"}
			wtc.Function.Name = tc.Name
			if tc.Arguments == "" {
				wtc.Function.Arguments = "{}"
			} else {
				wtc.Function.Arguments = tc.Arguments
			}
			wm.ToolCalls = append(wm.ToolCalls, wtc)
		}
		out = append(out, wm)
	}
	return out
}

func toWireTools(tools []ToolDefinition) []wireTool {
	out := make([]wireTool, 0, len(tools))
	for _, t := range tools {
		wt := wireTool{Type: "function"}
		wt.Function.Name = t.Name
		wt.Function.Description = t.Description
		if len(t.Parameters) > 0 {
			wt.Function.Parameters = json.RawMessage(t.Parameters)
		} else {
			wt.Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, wt)
	}
	return out
}
