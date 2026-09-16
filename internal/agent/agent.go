package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/mincode/mincode/internal/ctxmgr"
	"github.com/mincode/mincode/internal/llm"
	"github.com/mincode/mincode/internal/observability"
	"github.com/mincode/mincode/internal/permission"
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

// truncatePreview cuts s to at most max bytes on a UTF-8 rune boundary.
func truncatePreview(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "..."
}

// Agent coordinates the loop; it does not touch the filesystem or shell directly.
type Agent struct {
	Provider  llm.Provider
	Tools     *tools.Registry
	Bus       *observability.Bus
	SessionID string
	MaxSteps  int

	// Policy and Approver gate write/edit tools. Nil policy uses defaults.
	Policy   permission.Policy
	Approver permission.Approver

	// Ctx builds budgeted prompts and keeps conversation state.
	Ctx   *ctxmgr.Manager
	State State
}

// New creates an agent with a context manager.
func New(provider llm.Provider, reg *tools.Registry, bus *observability.Bus, sessionID string, maxSteps int, systemPrompt string, tokenBudget int) *Agent {
	return NewWithCompress(provider, reg, bus, sessionID, maxSteps, systemPrompt, tokenBudget, ctxmgr.DefaultCompressAtTokens)
}

// NewWithCompress is New with an explicit compaction threshold.
func NewWithCompress(provider llm.Provider, reg *tools.Registry, bus *observability.Bus, sessionID string, maxSteps int, systemPrompt string, tokenBudget, compressAt int) *Agent {
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	mgr := ctxmgr.New(systemPrompt, "", tokenBudget)
	mgr.SetCompressAt(compressAt)
	return &Agent{
		Provider:  provider,
		Tools:     reg,
		Bus:       bus,
		SessionID: sessionID,
		MaxSteps:  maxSteps,
		Policy:    &permission.ShellAwarePolicy{Inner: permission.NewDefaultPolicy()},
		Ctx:       mgr,
		State:     StateIdle,
	}
}

// ClearConversation drops user/assistant/tool turns but keeps system prompt.
func (a *Agent) ClearConversation() {
	a.Ctx.Clear()
	a.State = StateIdle
}

func (a *Agent) emit(typ observability.EventType, data any) {
	if a.Bus == nil {
		return
	}
	a.Bus.Publish(observability.NewEvent(a.SessionID, a.Ctx.Len(), typ, data))
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
	Snapshot  *ctxmgr.Snapshot
}

// Run processes one user message through the agent loop.
func (a *Agent) Run(ctx context.Context, userInput string) (*Result, error) {
	a.Ctx.AppendUser(userInput)

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
			a.Ctx.DropLastUser()
			return nil, err
		}

		a.setState(StateBuildingContext)
		a.emit(observability.EventContextBuildStarted, observability.ContextBuiltData{
			Step:   step + 1,
			Budget: a.Ctx.Budget(),
		})

		if cr := a.Ctx.CompactIfNeed(); cr != nil {
			a.emit(observability.EventContextCompactionStart, observability.CompactionData{
				BeforeTokens: cr.BeforeTokens,
			})
			preview := cr.Summary
			if len(preview) > 300 {
				preview = truncatePreview(preview, 300)
			}
			a.emit(observability.EventContextCompacted, observability.CompactionData{
				BeforeTokens:   cr.BeforeTokens,
				AfterTokens:    cr.AfterTokens,
				Compressed:     cr.Compressed,
				Preserved:      cr.Preserved,
				Pinned:         cr.Pinned,
				SummaryPreview: preview,
			})
		}

		defs := a.toolDefinitions()
		req, snap := a.Ctx.BuildRequest(defs)
		a.emit(observability.EventContextBuilt, observability.ContextBuiltData{
			Step:        snap.Step,
			TotalTokens: snap.TotalTokens,
			ToolTokens:  snap.ToolTokens,
			Budget:      snap.Budget,
			Included:    snap.Included,
			Excluded:    snap.Excluded,
			Truncated:   snap.Truncated,
		})

		a.setState(StateCallingLLM)
		start := time.Now()
		a.emitLLMStarted(req)
		resp, err := a.Provider.Chat(ctx, req)
		elapsed := time.Since(start)
		if err != nil {
			a.emitLLMFailed(elapsed, err)
			a.Ctx.MarkRequestFailed()
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				a.setState(StateCancelled)
			} else {
				a.setState(StateFailed)
			}
			a.Ctx.DropLastUser()
			return nil, err
		}
		a.emitLLMFinished(req, resp, elapsed)

		// Calibrate local token estimates against provider-reported prompt_tokens.
		if resp.Usage.PromptTokens > 0 {
			est := snap.TotalTokens + snap.ToolTokens
			a.Ctx.ObserveUsage(est, resp.Usage.PromptTokens)
		}

		a.setState(StateProcessingResponse)

		if len(resp.ToolCalls) == 0 {
			a.Ctx.AppendAssistant(llm.Message{
				Role:    llm.RoleAssistant,
				Content: resp.Content,
			})
			a.setState(StateFinished)
			return &Result{
				Final:     resp.Content,
				Steps:     steps,
				ToolCalls: toolCalls,
				State:     StateFinished,
				Snapshot:  a.Ctx.LastSnapshot(),
			}, nil
		}

		a.Ctx.AppendAssistant(llm.Message{
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
					a.Ctx.DropLastUser()
					return nil, execErr
				}
				result = tools.Result{Content: execErr.Error(), IsError: true}
			}

			a.Ctx.AppendToolResult(tc.ID, result.Content)
		}
	}

	a.setState(StateMaxStepsReached)
	return &Result{
		Steps:     steps,
		ToolCalls: toolCalls,
		State:     StateMaxStepsReached,
		Snapshot:  a.Ctx.LastSnapshot(),
	}, fmt.Errorf("%w: %d", MaxStepsExceeded, a.MaxSteps)
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

	summary := summarizeToolCall(tc.Name, args)
	req := permission.Request{
		Tool:      tc.Name,
		Arguments: args,
		Summary:   summary,
	}
	level := permission.Ask
	if a.Policy != nil {
		level = a.Policy.Evaluate(req)
	}
	a.emit(observability.EventPermissionRequested, observability.PermissionData{
		Tool:      tc.Name,
		Arguments: args,
		Summary:   summary,
		Level:     level.String(),
	})

	denied, perr := permission.Evaluate(a.Policy, a.Approver, req)
	if denied {
		reason := "denied"
		if perr != nil {
			reason = perr.Error()
		}
		a.emit(observability.EventPermissionDenied, observability.PermissionData{
			Tool:     tc.Name,
			Summary:  summary,
			Level:    level.String(),
			Decision: "denied",
			Reason:   reason,
		})
		msg := "permission denied: " + reason
		return tools.Result{Content: msg, IsError: true, Meta: map[string]any{"permission": "denied"}}, nil
	}
	a.emit(observability.EventPermissionApproved, observability.PermissionData{
		Tool:     tc.Name,
		Summary:  summary,
		Level:    level.String(),
		Decision: "approved",
	})

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

	if metaPath, ok := result.Meta["path"].(string); ok && (tc.Name == "write_file" || tc.Name == "edit_file") {
		if !result.IsError {
			op, _ := result.Meta["operation"].(string)
			diff, _ := result.Meta["diff"].(string)
			a.emit(observability.EventFileChanged, observability.FileChangedData{
				Path:      metaPath,
				Operation: op,
				Bytes:     len(result.Content),
				Diff:      diff,
			})
		}
	}

	preview := result.Content
	if len(preview) > toolPreviewLen {
		preview = truncatePreview(preview, toolPreviewLen)
	}
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
	a.emit(observability.EventToolFinished, data)
	return result, nil
}

func summarizeToolCall(name, args string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return name
	}
	path, _ := m["path"].(string)
	switch name {
	case "write_file":
		return fmt.Sprintf("write_file %s", path)
	case "edit_file":
		return fmt.Sprintf("edit_file %s", path)
	case "read_file":
		return fmt.Sprintf("read_file %s", path)
	case "shell":
		cmd, _ := m["command"].(string)
		return "shell " + cmd
	default:
		if path != "" {
			return name + " " + path
		}
		return name
	}
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
		preview = truncatePreview(preview, toolPreviewLen)
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
