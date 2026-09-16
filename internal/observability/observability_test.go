package observability

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBusPublishSubscribeOrder(t *testing.T) {
	bus := NewBus()
	var got []EventType
	bus.Subscribe(func(e Event) { got = append(got, e.Type) })
	bus.Subscribe(func(e Event) { got = append(got, e.Type) })

	bus.Publish(NewEvent("s1", 1, EventAgentStarted, nil))
	if len(got) != 2 {
		t.Fatalf("handlers called = %d, want 2", len(got))
	}
	if got[0] != EventAgentStarted || got[1] != EventAgentStarted {
		t.Fatalf("got = %v", got)
	}
}

func TestNewEventFields(t *testing.T) {
	e := NewEvent("sess", 3, EventLLMRequestStarted, LLMRequestData{Model: "m"})
	if e.ID == "" {
		t.Fatal("empty id")
	}
	if e.SessionID != "sess" || e.Step != 3 || e.Type != EventLLMRequestStarted {
		t.Fatalf("event = %+v", e)
	}
	if e.Time.IsZero() {
		t.Fatal("zero time")
	}
}

func TestRecorderJSONLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")

	rec, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}

	e1 := NewEvent("sess", 0, EventSessionCreated, SessionCreatedData{Model: "m", Provider: "fake"})
	e2 := NewEvent("sess", 1, EventLLMRequestFinished, LLMRequestData{
		Model: "m", InputTokens: 10, OutputTokens: 5, TotalTokens: 15, DurationMS: 12,
	})
	if err := rec.Record(e1); err != nil {
		t.Fatal(err)
	}
	if err := rec.Record(e2); err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	events, err := ReadEvents(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].Type != EventSessionCreated {
		t.Fatalf("e0 type = %s", events[0].Type)
	}
	if events[1].Type != EventLLMRequestFinished {
		t.Fatalf("e1 type = %s", events[1].Type)
	}

	// Data survives as map after JSON decode.
	data, ok := events[1].Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T", events[1].Data)
	}
	if data["total_tokens"] != float64(15) {
		t.Fatalf("total_tokens = %v", data["total_tokens"])
	}
}

func TestMetricsFromFinishedAndFailed(t *testing.T) {
	c := NewMetricsCollector()
	c.Handle(NewEvent("s", 1, EventLLMRequestFinished, LLMRequestData{
		InputTokens: 100, OutputTokens: 20, TotalTokens: 120, DurationMS: 50,
	}))
	c.Handle(NewEvent("s", 2, EventLLMRequestFinished, LLMRequestData{
		InputTokens: 50, OutputTokens: 10, TotalTokens: 60, DurationMS: 30,
	}))
	c.Handle(NewEvent("s", 3, EventLLMRequestFailed, LLMRequestData{
		DurationMS: 10, Error: "boom",
	}))

	m := c.Snapshot()
	if m.LLMCalls != 2 {
		t.Fatalf("calls = %d", m.LLMCalls)
	}
	if m.Errors != 1 {
		t.Fatalf("errors = %d", m.Errors)
	}
	if m.InputTokens != 150 || m.OutputTokens != 30 || m.TotalTokens != 180 {
		t.Fatalf("tokens = %+v", m)
	}
	if m.LLMDuration != 90*time.Millisecond {
		t.Fatalf("duration = %v", m.LLMDuration)
	}

	if m.Format() == "" {
		t.Fatal("empty format")
	}
}

func TestRecorderCreatesParentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "traces")
	path := filepath.Join(dir, "x.jsonl")
	rec, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
