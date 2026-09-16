package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mincode/mincode/internal/llm"
	"github.com/mincode/mincode/internal/observability"
	"github.com/mincode/mincode/internal/tools"
)

// MaxStepsExceeded is returned when the loop hits the step budget.
var MaxStepsExceeded = errors.New("agent: max steps exceeded")

// LoopDetected is returned when the same tool call repeats too often.
var LoopDetected = errors.New("agent: tool call loop detected")

const (
	defaultMaxSteps = 30
	maxLoopRepeats  = 5
	toolPreviewLen  = 200
)

// Agent coordinates the loop; it does not touch the filesystem or shell directly.
type Agent struct {
	Provider  llm.Provider
	Tools     *tools.Registry
	Bus       *observability.Bus
	SessionID string
	MaxSteps  int

	// History is the in-memory conversation (Phase 3 will replace with Context Manager).
	History []llm.Message
	State   State
}

// New creates a Phase 2 read-only agent.
func New(provider llm.Provider, reg *tools.Registry, bus *observability.Bus, sessionID string, maxSteps int, systemPrompt string) *Agent {
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	history := []llm.Message{}
	if systemPrompt != "" {
		history = append(history, llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	}
	return &Agent{
		Provider:  provider,
		Tools:     reg,
		Bus:       bus,
		SessionID: sessionID,
		MaxSteps:  maxSteps,
		History:   history,
		State:     StateIdle,
	}
}

// ResetHistory keeps the system prompt and drops the rest.
func (a *Agent) ResetHistory(systemPrompt string) {
	a.History = nil
	if systemPrompt != "" {
		a.History = append(a.History, llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	}
	a.State = StateIdle
}

// ClearConversation drops user/assistant/tool turns but keeps system prompt.
func (a *Agent) ClearConversation() {
	if len(a.History) > 0 && a.History[0].Role == llm.RoleSystem {
		a.History = a.History[:1]
	} else {
		a.History = nil
	}
	a.State = StateIdle
}

func (a *Agent) emit(typ observability.EventType, data any) {
	if a.Bus == nil {
		return
	}
	a.Bus.Publish(observability.NewEvent(a.SessionID, len(a.History), typ, data))
}

func (a *Agent) setState(to State) {
	from := a.State
	if from == to {
		return
	}
	a.State = to
	a.emit(observability.EventAgentStateChanged, observability.StateChangedData{
		From: string(from),
		To:   string(to),
	})
}

// Result is the outcome of one user turn.
type Result struct {
	Final     string
	Steps     int
	ToolCalls int
	State     State
}

// Run processes one user message through the agent loop.
func (a *Agent) Run(ctx context.Context, userInput string) (*Result, error) {
	a.History = append(a.History, llm.Message{Role: llm.RoleUser, Content: userInput})

	var (
		steps     int
		toolCalls int
		lastKey   string
		repeats   int
	)

	for step := 0; step < a.MaxSteps; step++ {
		steps = step + 1

		if err := ctx.Err(); err != nil {
			a.setState(StateCancelled)
			// Drop the failed user turn so retry is clean.
			a.dropLastUser()
			return nil, err
		}

		a.setState(StateBuildingContext)
		req := llm.ChatRequest{
			Messages: a.History,
			Tools:    a.toolDefinitions(),
		}

		a.setState(StateCallingLLM)
		start := time.Now()
		a.emitLLMStarted(req)
		resp, err := a.Provider.Chat(ctx, req)
		elapsed := time.Since(start)
		if err != nil {
			a.emitLLMFailed(elapsed, err)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				a.setState(StateCancelled)
			} else {
				a.setState(StateFailed)
			}
			a.dropLastUser()
			return nil, err
		}
		a.emitLLMFinished(req, resp, elapsed)

		a.setState(StateProcessingResponse)

		// Final answer: content and no tool calls.
		if len(resp.ToolCalls) == 0 {
			a.History = append(a.History, llm.Message{
				Role:    llm.RoleAssistant,
				Content: resp.Content,
			})
			a.setState(StateFinished)
			return &Result{Final: resp.Content, Steps: steps, ToolCalls: toolCalls, State: StateFinished}, nil
		}

		// Record assistant turn (may include text + tool calls).
		a.History = append(a.History, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			toolCalls++

			key := tc.Name + "\x00" + tc.Arguments
			if key == lastKey {
				repeats++
			} else {
				lastKey = key
				repeats = 1
			}
			if repeats >= maxLoopRepeats {
				a.emit(observability.EventLoopDetected, observability.LoopDetectedData{
					Tool:      tc.Name,
					Count:     repeats,
					Arguments: tc.Arguments,
				})
				a.setState(StateFailed)
				return &Result{State: StateFailed}, fmt.Errorf("%w: %s x%d", LoopDetected, tc.Name, repeats)
			}

			a.setState(StateExecutingTool)
			result, execErr := a.executeTool(ctx, tc)
			if execErr != nil {
				if errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
					a.setState(StateCancelled)
					a.dropLastUser()
					return nil, execErr
				}
				// Convert unexpected errors into tool error messages for the model.
				result = tools.Result{Content: execErr.Error(), IsError: true}
			}

			a.History = append(a.History, llm.Message{
				Role:       llm.RoleTool,
				Content:    result.Content,
				ToolCallID: tc.ID,
			})
		}
	}

	a.setState(StateMaxStepsReached)
	return &Result{Steps: steps, ToolCalls: toolCalls, State: StateMaxStepsReached}, fmt.Errorf("%w: %d", MaxStepsExceeded, a.MaxSteps)
}

func (a *Agent) dropLastUser() {
	if n := len(a.History); n > 0 && a.History[n-1].Role == llm.RoleUser {
		a.History = a.History[:n-1]
	}
}

func (a *Agent) toolDefinitions() []llm.ToolDefinition {
	if a.Tools == nil {
		return nil
	}
	names := a.Tools.Names()
	out := make([]llm.ToolDefinition, 0, len(names))
	for _, name := range names {
		t, ok := a.Tools.Get(name)
		if !ok {
			continue
		}
		out = append(out, llm.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Schema().Raw(),
		})
	}
	return out
}

func (a *Agent) executeTool(ctx context.Context, tc llm.ToolCall) (tools.Result, error) {
	args := tc.Arguments
	if args == "" {
		args = "{}"
	}

	a.emit(observability.EventToolRequested, observability.ToolEventData{
		Tool:      tc.Name,
		Arguments: args,
	})
	a.emit(observability.EventToolStarted, observability.ToolEventData{
		Tool:      tc.Name,
		Arguments: args,
	})

	start := time.Now()
	result, err := a.Tools.Execute(ctx, tc.Name, json.RawMessage(args))
	elapsed := time.Since(start)

	if err != nil {
		a.emit(observability.EventToolFailed, observability.ToolEventData{
			Tool:       tc.Name,
			Arguments:  args,
			DurationMS: elapsed.Milliseconds(),
			Error:      err.Error(),
		})
		return result, err
	}

	preview := result.Content
	if len(preview) > toolPreviewLen {
		preview = preview[:toolPreviewLen] + "..."
	}
	evType := observability.EventToolFinished
	data := observability.ToolEventData{
		Tool:          tc.Name,
		Arguments:     args,
		DurationMS:    elapsed.Milliseconds(),
		ResultSize:    len(result.Content),
		IsError:       result.IsError,
		OutputPreview: preview,
	}
	if result.IsError {
		data.Error = result.Content
	}
	a.emit(evType, data)
	return result, nil
}

func (a *Agent) emitLLMStarted(req llm.ChatRequest) {
	a.emit(observability.EventLLMRequestStarted, observability.LLMRequestData{
		Provider:     a.Provider.Name(),
		Model:        a.Provider.Model(),
		MessageCount: len(req.Messages),
	})
}

func (a *Agent) emitLLMFinished(req llm.ChatRequest, resp *llm.ChatResponse, elapsed time.Duration) {
	preview := resp.Content
	if len(preview) > toolPreviewLen {
		preview = preview[:toolPreviewLen] + "..."
	}
	if preview == "" && len(resp.ToolCalls) > 0 {
		preview = fmt.Sprintf("tool_calls: %d", len(resp.ToolCalls))
	}
	a.emit(observability.EventLLMRequestFinished, observability.LLMRequestData{
		Provider:       a.Provider.Name(),
		Model:          a.Provider.Model(),
		MessageCount:   len(req.Messages),
		InputTokens:    resp.Usage.PromptTokens,
		OutputTokens:   resp.Usage.CompletionTokens,
		TotalTokens:    resp.Usage.TotalTokens,
		DurationMS:     elapsed.Milliseconds(),
		ContentPreview: preview,
	})
}

func (a *Agent) emitLLMFailed(elapsed time.Duration, err error) {
	a.emit(observability.EventLLMRequestFailed, observability.LLMRequestData{
		Provider:   a.Provider.Name(),
		Model:      a.Provider.Model(),
		DurationMS: elapsed.Milliseconds(),
		Error:      err.Error(),
	})
}
