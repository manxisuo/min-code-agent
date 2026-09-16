package ctxmgr

import (
	"strings"
	"testing"

	"github.com/mincode/mincode/internal/llm"
)

func TestSanitizeToolPairsDropsOrphans(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "hi"},
		// orphan tool result with no preceding assistant tool_calls
		{Role: llm.RoleTool, Content: "orphan", ToolCallID: "x"},
		{Role: llm.RoleAssistant, Content: "ok"},
	}
	out := sanitizeToolPairs(msgs)
	for _, m := range out {
		if m.Role == llm.RoleTool {
			t.Fatalf("orphan tool not dropped: %+v", out)
		}
	}
	if len(out) != 3 {
		t.Fatalf("len = %d: %+v", len(out), out)
	}
}

func TestSanitizeToolPairsKeepsValid(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "go"},
		{
			Role:      llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{ID: "c1", Name: "shell", Arguments: "{}"}},
		},
		{Role: llm.RoleTool, Content: "ok", ToolCallID: "c1"},
		{Role: llm.RoleAssistant, Content: "done"},
	}
	out := sanitizeToolPairs(msgs)
	if len(out) != 4 {
		t.Fatalf("len=%d %+v", len(out), out)
	}
	if out[2].Role != llm.RoleTool || out[2].ToolCallID != "c1" {
		t.Fatalf("tool msg = %+v", out[2])
	}
}

func TestSanitizeToolPairsFillsMissing(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "go"},
		{
			Role:      llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{ID: "c1", Name: "shell"}, {ID: "c2", Name: "shell"}},
		},
		{Role: llm.RoleTool, Content: "only one", ToolCallID: "c1"},
		// c2 missing — then a new user turn
		{Role: llm.RoleUser, Content: "next"},
	}
	out := sanitizeToolPairs(msgs)
	found := false
	for _, m := range out {
		if m.Role == llm.RoleTool && m.ToolCallID == "c2" {
			found = true
			if !strings.Contains(m.Content, "missing") {
				t.Fatalf("placeholder = %q", m.Content)
			}
		}
	}
	if !found {
		t.Fatalf("missing placeholder for c2: %+v", out)
	}
}

func TestBuildRequestAlwaysValidToolPairs(t *testing.T) {
	m := New("SYS", "", 32000)
	m.AppendUser("do it")
	m.AppendAssistant(llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "c1", Name: "shell", Arguments: `{"command":"echo"}`}},
	})
	m.AppendToolResult("c1", "ok")

	// Force tiny budget so some history may be dropped.
	m.SetBudget(5)
	req, _ := m.BuildRequest(nil)

	pending := map[string]bool{}
	for i, msg := range req.Messages {
		switch msg.Role {
		case llm.RoleAssistant:
			pending = map[string]bool{}
			for _, tc := range msg.ToolCalls {
				pending[tc.ID] = true
			}
		case llm.RoleTool:
			if !pending[msg.ToolCallID] {
				t.Fatalf("orphan tool at %d: %+v\nall=%+v", i, msg, req.Messages)
			}
			delete(pending, msg.ToolCallID)
		case llm.RoleUser, llm.RoleSystem:
			// pending should have been flushed with placeholders already in stream
			_ = pending
		}
	}
}
