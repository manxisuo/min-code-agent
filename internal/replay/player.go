package replay

import (
	"fmt"
	"io"
	"strings"

	"github.com/mincode/mincode/internal/observability"
)

// Player steps through a trace file interactively or programmatically.
type Player struct {
	events []observability.Event
	pos    int // 0-based index of current event
}

// Load reads a trace JSONL file.
func Load(path string) (*Player, error) {
	events, err := observability.ReadEvents(path)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("trace %s has no events", path)
	}
	return &Player{events: events, pos: -1}, nil
}

// Len returns event count.
func (p *Player) Len() int { return len(p.events) }

// Index returns the 1-based current position (0 if not started).
func (p *Player) Index() int {
	if p.pos < 0 {
		return 0
	}
	return p.pos + 1
}

// Current returns the current event.
func (p *Player) Current() (observability.Event, bool) {
	if p.pos < 0 || p.pos >= len(p.events) {
		return observability.Event{}, false
	}
	return p.events[p.pos], true
}

// Next advances one step. Returns false at end.
func (p *Player) Next() bool {
	if p.pos+1 >= len(p.events) {
		return false
	}
	p.pos++
	return true
}

// Prev goes back one step. Returns false at start.
func (p *Player) Prev() bool {
	if p.pos <= 0 {
		return false
	}
	p.pos--
	return true
}

// Goto moves to 1-based index. Returns false if out of range.
func (p *Player) Goto(n int) bool {
	if n < 1 || n > len(p.events) {
		return false
	}
	p.pos = n - 1
	return true
}

// Summary prints an overview of the whole trace.
func (p *Player) Summary(w io.Writer) {
	counts := map[observability.EventType]int{}
	for _, e := range p.events {
		counts[e.Type]++
	}
	fmt.Fprintf(w, "Replay summary  (%d events)\n\n", len(p.events))
	order := []observability.EventType{
		observability.EventSessionCreated,
		observability.EventAgentStarted,
		observability.EventAgentFinished,
		observability.EventAgentStateChanged,
		observability.EventContextBuilt,
		observability.EventLLMRequestStarted,
		observability.EventLLMRequestFinished,
		observability.EventLLMRequestFailed,
		observability.EventPermissionRequested,
		observability.EventPermissionApproved,
		observability.EventPermissionDenied,
		observability.EventToolStarted,
		observability.EventToolFinished,
		observability.EventToolFailed,
		observability.EventFileChanged,
		observability.EventLoopDetected,
	}
	for _, typ := range order {
		if n := counts[typ]; n > 0 {
			fmt.Fprintf(w, "  %-24s %d\n", typ, n)
		}
	}
	// Any remaining types
	for typ, n := range counts {
		found := false
		for _, o := range order {
			if o == typ {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(w, "  %-24s %d\n", typ, n)
		}
	}
	first, last := p.events[0], p.events[len(p.events)-1]
	fmt.Fprintf(w, "\n  span  %s → %s\n",
		first.Time.Local().Format("15:04:05"),
		last.Time.Local().Format("15:04:05"))
}

// PrintCurrent renders the current event.
func (p *Player) PrintCurrent(w io.Writer) {
	e, ok := p.Current()
	if !ok {
		fmt.Fprintln(w, "(no event)")
		return
	}
	fmt.Fprintf(w, "\nStep %d / %d\n\n", p.pos+1, len(p.events))
	fmt.Fprintf(w, "  Time:    %s\n", e.Time.Local().Format("2006-01-02 15:04:05.000"))
	fmt.Fprintf(w, "  Type:    %s\n", e.Type)
	fmt.Fprintf(w, "  Session: %s\n", e.SessionID)
	if e.Step > 0 {
		fmt.Fprintf(w, "  Step:    %d\n", e.Step)
	}
	if e.Data != nil {
		fmt.Fprintf(w, "  Data:    %s\n", formatData(e.Data))
	}
	fmt.Fprintln(w)
}

func formatData(v any) string {
	s := fmt.Sprintf("%v", v)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
