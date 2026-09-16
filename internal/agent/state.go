// Package agent implements the Phase 2+3 agent loop with context management.
package agent

// State is the agent lifecycle state.
type State string

const (
	StateIdle               State = "IDLE"
	StateBuildingContext    State = "BUILDING_CONTEXT"
	StateCallingLLM         State = "CALLING_LLM"
	StateProcessingResponse State = "PROCESSING_RESPONSE"
	StateExecutingTool      State = "EXECUTING_TOOL"
	StateFinished           State = "FINISHED"
	StateFailed             State = "FAILED"
	StateCancelled          State = "CANCELLED"
	StateMaxStepsReached    State = "MAX_STEPS_REACHED"
)

func (s State) String() string { return string(s) }
