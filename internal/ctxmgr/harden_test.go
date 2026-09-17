package ctxmgr

import (
	"fmt"
	"strings"
	"testing"

	"github.com/manxisuo/mincode/internal/llm"
)

// TestLongSessionCompressionKeepsPinned ensures a long tool-heavy conversation
// compresses without dropping system/instructions/skills, and tool pairs stay valid.
func TestLongSessionCompressionKeepsPinned(t *testing.T) {
	m := New("SYS-PROMPT", "INSTR-ALWAYS", 8000)
	m.SetSkills("SKILL-GO-TESTING")
	m.SetCompressAt(2000)

	// Simulate many tool rounds to force compaction.
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("call_%d", i)
		m.AppendAssistant(llm.Message{
			Role:    llm.RoleAssistant,
			Content: "using tool",
			ToolCalls: []llm.ToolCall{
				{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.go"}`, i)},
			},
		})
		body := strings.Repeat(fmt.Sprintf("file content block %d\n", i), 20)
		m.AppendToolResult(id, body)
	}
	m.AppendUser("what next?")

	cr := m.CompactIfNeed()
	if cr == nil {
		t.Fatal("expected compaction on long session")
	}

	req, snap := m.BuildRequest(nil)
	if len(req.Messages) == 0 {
		t.Fatal("empty request")
	}

	var systemText string
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleSystem {
			systemText += msg.Content + "\n"
		}
	}
	if !strings.Contains(systemText, "SYS-PROMPT") {
		t.Fatalf("system prompt lost after compress:\n%s", systemText)
	}
	if !strings.Contains(systemText, "INSTR-ALWAYS") {
		t.Fatalf("instructions lost after compress:\n%s", systemText)
	}
	if !strings.Contains(systemText, "SKILL-GO-TESTING") {
		t.Fatalf("skills lost after compress:\n%s", systemText)
	}

	// Tool messages must still be paired (sanitizeToolPairs).
	pending := map[string]bool{}
	for _, msg := range req.Messages {
		switch msg.Role {
		case llm.RoleAssistant:
			pending = map[string]bool{}
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					pending[tc.ID] = true
				}
			}
		case llm.RoleTool:
			if !pending[msg.ToolCallID] {
				t.Fatalf("orphan tool message tool_call_id=%q", msg.ToolCallID)
			}
			delete(pending, msg.ToolCallID)
		}
	}

	if snap.TotalTokens <= 0 {
		t.Fatal("snapshot tokens missing")
	}
	// Compression should have reduced stored history.
	if m.Len() >= 80 {
		t.Fatalf("history not compressed enough: len=%d", m.Len())
	}
}

// TestBuildRequestOverBudgetDropsOldest keeps the newest user input.
func TestBuildRequestOverBudgetDropsOldest(t *testing.T) {
	m := New("sys", "", 2000)
	for i := 0; i < 30; i++ {
		m.AppendUser(fmt.Sprintf("question %d %s", i, strings.Repeat("x", 200)))
		m.AppendAssistant(llm.Message{Role: llm.RoleAssistant, Content: strings.Repeat("a", 200)})
	}
	m.AppendUser("FINAL-QUESTION")

	req, snap := m.BuildRequest(nil)
	foundFinal := false
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, "FINAL-QUESTION") {
			foundFinal = true
		}
	}
	if !foundFinal {
		t.Fatal("latest user input dropped under budget")
	}
	if snap.Excluded == 0 {
		t.Fatal("expected exclusions under tight budget")
	}
	// System must survive.
	if !strings.Contains(req.Messages[0].Content, "sys") {
		t.Fatalf("system missing: %+v", req.Messages[0])
	}
}
