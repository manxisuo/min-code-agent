package observability

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// EventType identifies a structured observation.
type EventType string

const (
	EventSessionCreated EventType = "session.created"

	EventAgentStarted  EventType = "agent.started"
	EventAgentFinished EventType = "agent.finished"
	EventAgentFailed   EventType = "agent.failed"

	EventContextBuildStarted    EventType = "context.build_started"
	EventContextBuilt           EventType = "context.built"
	EventContextCompactionStart EventType = "context.compaction_started"
	EventContextCompacted       EventType = "context.compacted"

	EventLLMRequestStarted  EventType = "llm.request_started"
	EventLLMRequestFinished EventType = "llm.request_finished"
	EventLLMRequestFailed   EventType = "llm.request_failed"

	EventAgentStateChanged EventType = "agent.state_changed"

	EventToolRequested EventType = "tool.requested"
	EventToolStarted   EventType = "tool.started"
	EventToolFinished  EventType = "tool.finished"
	EventToolFailed    EventType = "tool.failed"

	EventToolBatchStarted  EventType = "tool.batch_started"
	EventToolBatchFinished EventType = "tool.batch_finished"

	EventPermissionRequested EventType = "permission.requested"
	EventPermissionApproved  EventType = "permission.approved"
	EventPermissionDenied    EventType = "permission.denied"

	EventFileChanged EventType = "file.changed"

	EventInstructionLoaded EventType = "instruction.loaded"
	EventSkillLoaded       EventType = "skill.loaded"
	EventSkillUnloaded     EventType = "skill.unloaded"
	EventMemoryRetrieved   EventType = "memory.retrieved"
	EventMemoryUpdated     EventType = "memory.updated"

	EventPlanCreated      EventType = "plan.created"
	EventPlanApproved     EventType = "plan.approved"
	EventPlanRejected     EventType = "plan.rejected"
	EventPlanCancelled    EventType = "plan.cancelled"
	EventPlanStepStarted  EventType = "plan.step_started"
	EventPlanStepFinished EventType = "plan.step_finished"
	EventPlanStepFailed   EventType = "plan.step_failed"
	EventPlanFinished     EventType = "plan.finished"

	EventLoopDetected EventType = "loop.detected"
)

// Event is a structured observation record.
type Event struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	SessionID string    `json:"session_id"`
	Step      int       `json:"step"`
	Type      EventType `json:"type"`
	Data      any       `json:"data,omitempty"`
}

// NewEvent builds an event with a generated id and timestamp.
func NewEvent(sessionID string, step int, typ EventType, data any) Event {
	return Event{
		ID:        NewID(),
		Time:      time.Now().UTC(),
		SessionID: sessionID,
		Step:      step,
		Type:      typ,
		Data:      data,
	}
}

// NewID returns a random 16-byte hex id.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is fatal in practice; fall back to timestamp.
		return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b[:])
}

// LLMRequestData is payload for llm.request_* events.
type LLMRequestData struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	MessageCount int    `json:"message_count"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
	// Output fields on finished events.
	ContentPreview string `json:"content_preview,omitempty"`
	OutputTokens   int    `json:"output_tokens,omitempty"`
	TotalTokens    int    `json:"total_tokens,omitempty"`
	Error          string `json:"error,omitempty"`
}

// AgentLifecycleData is payload for agent.* events.
type AgentLifecycleData struct {
	Reason string `json:"reason,omitempty"`
}

// SessionCreatedData is payload for session.created.
type SessionCreatedData struct {
	Workspace string `json:"workspace,omitempty"`
	Model     string `json:"model,omitempty"`
	Provider  string `json:"provider,omitempty"`
}

// StateChangedData is payload for agent.state_changed.
type StateChangedData struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ContextBuiltData is payload for context.build_started / context.built.
type ContextBuiltData struct {
	Step        int `json:"step"`
	TotalTokens int `json:"total_tokens"`
	ToolTokens  int `json:"tool_tokens"`
	Budget      int `json:"budget"`
	Included    int `json:"included_count"`
	Excluded    int `json:"excluded_count"`
	Truncated   int `json:"truncated_count"`
}

// CompactionData is payload for context.compaction_*.
type CompactionData struct {
	BeforeTokens   int    `json:"before_tokens"`
	AfterTokens    int    `json:"after_tokens"`
	Dropped        int    `json:"dropped"`
	Compressed     int    `json:"compressed"`
	Preserved      int    `json:"preserved"`
	Pinned         int    `json:"pinned"`
	SummaryPreview string `json:"summary_preview,omitempty"`
}

// ToolEventData is payload for tool.* events.
type ToolEventData struct {
	Tool          string `json:"tool"`
	Arguments     string `json:"arguments,omitempty"`
	CallID        string `json:"call_id,omitempty"`
	Index         int    `json:"index,omitempty"`
	Parallel      bool   `json:"parallel,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	ResultSize    int    `json:"result_size,omitempty"`
	IsError       bool   `json:"is_error,omitempty"`
	Error         string `json:"error,omitempty"`
	OutputPreview string `json:"output_preview,omitempty"`
}

// ToolBatchData is payload for tool.batch_* events.
type ToolBatchData struct {
	Size       int      `json:"size"`
	Parallel   bool     `json:"parallel"`
	Tools      []string `json:"tools,omitempty"`
	CallIDs    []string `json:"call_ids,omitempty"`
	DurationMS int64    `json:"duration_ms,omitempty"`
	Succeeded  int      `json:"succeeded,omitempty"`
	Failed     int      `json:"failed,omitempty"`
	MaxWorkers int      `json:"max_workers,omitempty"`
}

// LoopDetectedData is payload for loop.detected.
type LoopDetectedData struct {
	Tool      string `json:"tool"`
	Count     int    `json:"count"`
	Arguments string `json:"arguments,omitempty"`
}

// PermissionData is payload for permission.* events.
type PermissionData struct {
	Tool      string `json:"tool"`
	Arguments string `json:"arguments,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Level     string `json:"level,omitempty"`
	Decision  string `json:"decision,omitempty"` // approved | denied
	Reason    string `json:"reason,omitempty"`
}

// FileChangedData is payload for file.changed.
type FileChangedData struct {
	Path      string `json:"path"`
	Operation string `json:"operation"` // created | overwrote | edit
	Bytes     int    `json:"bytes,omitempty"`
	Diff      string `json:"diff,omitempty"`
}

// InstructionLoadedData is payload for instruction.loaded.
type InstructionLoadedData struct {
	Path    string `json:"path"`
	RelPath string `json:"rel_path"`
	RelDir  string `json:"rel_dir"`
	Bytes   int    `json:"bytes"`
	Content string `json:"content,omitempty"`
}

// SkillEventData is payload for skill.loaded / skill.unloaded.
type SkillEventData struct {
	Name    string `json:"name"`
	RelPath string `json:"rel_path,omitempty"`
	Bytes   int    `json:"bytes,omitempty"`
	Summary string `json:"summary,omitempty"`
	Reason  string `json:"reason,omitempty"` // e.g. "user command"
}

// MemoryEventData is payload for memory.retrieved / memory.updated.
type MemoryEventData struct {
	RelPath string `json:"rel_path,omitempty"`
	Bytes   int    `json:"bytes,omitempty"`
	Entries int    `json:"entries,omitempty"`
	Entry   string `json:"entry,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// PlanEventData is payload for plan.* lifecycle events.
type PlanEventData struct {
	PlanID    string `json:"plan_id"`
	Goal      string `json:"goal,omitempty"`
	Status    string `json:"status,omitempty"`
	StepIndex int    `json:"step_index,omitempty"`
	StepTitle string `json:"step_title,omitempty"`
	StepCount int    `json:"step_count,omitempty"`
	DoneCount int    `json:"done_count,omitempty"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	Reason    string `json:"reason,omitempty"`
}
