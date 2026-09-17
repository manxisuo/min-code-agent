package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/manxisuo/mincode/internal/llm"
	"github.com/manxisuo/mincode/internal/observability"
	"github.com/manxisuo/mincode/internal/permission"
	"github.com/manxisuo/mincode/internal/tools"
)

func TestAgentWriteApprovedAndTraced(t *testing.T) {
	ws := testWorkspace(t)
	reg := tools.NewRegistry()
	reg.Register(&tools.WriteFile{WS: ws})
	reg.Register(&tools.ReadFile{WS: ws})

	bus := observability.NewBus()
	var types []observability.EventType
	bus.Subscribe(func(e observability.Event) { types = append(types, e.Type) })

	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{
				ID: "1", Name: "write_file",
				Arguments: `{"path":"note.txt","content":"hello phase4"}`,
			}}},
			{Content: "wrote note.txt"},
		},
	}
	ag := New(fake, reg, bus, "s", 5, "sys", 10000)
	ag.Approver = allowAll{}

	res, err := ag.Run(context.Background(), "write a note")
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != "wrote note.txt" {
		t.Fatalf("final = %q", res.Final)
	}
	data, err := os.ReadFile(filepath.Join(ws.Root(), "note.txt"))
	if err != nil || string(data) != "hello phase4" {
		t.Fatalf("file err=%v data=%q", err, data)
	}

	want := map[observability.EventType]bool{
		observability.EventPermissionRequested: false,
		observability.EventPermissionApproved:  false,
		observability.EventFileChanged:         false,
	}
	for _, typ := range types {
		if _, ok := want[typ]; ok {
			want[typ] = true
		}
	}
	for typ, ok := range want {
		if !ok {
			t.Fatalf("missing event %s in %v", typ, types)
		}
	}
}

func TestAgentWriteDenied(t *testing.T) {
	ws := testWorkspace(t)
	reg := tools.NewRegistry()
	reg.Register(&tools.WriteFile{WS: ws})
	bus := observability.NewBus()
	var types []observability.EventType
	bus.Subscribe(func(e observability.Event) { types = append(types, e.Type) })

	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{
				ID: "1", Name: "write_file",
				Arguments: `{"path":"evil.txt","content":"x"}`,
			}}},
			{Content: "could not write"},
		},
	}
	ag := New(fake, reg, bus, "s", 5, "sys", 10000)
	ag.Approver = allowAll{deny: true}

	res, err := ag.Run(context.Background(), "write")
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(ws.Root(), "evil.txt")); statErr == nil {
		t.Fatal("file should not exist after deny")
	}
	foundDeny := false
	for _, typ := range types {
		if typ == observability.EventPermissionDenied {
			foundDeny = true
		}
	}
	if !foundDeny {
		t.Fatalf("missing permission.denied: %v", types)
	}
	// Model still gets a tool result and can finish.
	if res.Final == "" {
		t.Fatal("expected final answer after denied tool")
	}
}

func TestAgentWriteNoApproverDenies(t *testing.T) {
	ws := testWorkspace(t)
	reg := tools.NewRegistry()
	reg.Register(&tools.WriteFile{WS: ws})
	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{
				ID: "1", Name: "write_file",
				Arguments: `{"path":"x.txt","content":"y"}`,
			}}},
			{Content: "done"},
		},
	}
	ag := New(fake, reg, observability.NewBus(), "s", 5, "sys", 10000)
	ag.Approver = nil
	if _, err := ag.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws.Root(), "x.txt")); err == nil {
		t.Fatal("write without approver must not create file")
	}
}

type allowAll struct{ deny bool }

func (a allowAll) Approve(req permission.Request) (bool, error) { return !a.deny, nil }
