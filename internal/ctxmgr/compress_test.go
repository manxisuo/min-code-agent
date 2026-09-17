package ctxmgr

import (
	"strings"
	"testing"

	"github.com/manxisuo/mincode/internal/llm"
)

func TestCompactReducesTokensAndKeepsRecent(t *testing.T) {
	m := New("SYS", "", 32000)
	m.SetCompressAt(100) // force

	for i := 0; i < 20; i++ {
		m.AppendUser("please analyze file " + strings.Repeat("x", 40))
		m.AppendAssistant(llm.Message{Role: llm.RoleAssistant, Content: "done analysis " + strings.Repeat("y", 40)})
	}

	before := m.Len()
	if before < 10 {
		t.Fatalf("len = %d", before)
	}

	res := m.Compact()
	if res == nil {
		t.Fatal("expected compaction")
	}
	if res.Compressed <= 0 || res.Preserved <= 0 {
		t.Fatalf("result = %+v", res)
	}
	if m.Len() >= before {
		t.Fatalf("entries not reduced: %d → %d", before, m.Len())
	}
	if !strings.Contains(res.Summary, "[Conversation Summary]") {
		t.Fatalf("summary = %s", res.Summary)
	}
	// First entry should be summary.
	if m.entries[0].source != SourceSummary {
		t.Fatalf("first source = %s", m.entries[0].source)
	}
	// Recent tail preserved: last assistant content still there.
	last := m.entries[len(m.entries)-1]
	if !strings.Contains(last.msg.Content, "done analysis") && last.msg.Role != llm.RoleAssistant {
		t.Fatalf("tail = %+v", last)
	}
}

func TestCompactIfNeedThreshold(t *testing.T) {
	m := New("SYS", "", 32000)
	m.SetCompressAt(100000)
	m.AppendUser("hi")
	if m.CompactIfNeed() != nil {
		t.Fatal("should not compact under threshold")
	}

	m.SetCompressAt(1)
	for i := 0; i < 10; i++ {
		m.AppendUser("msg")
		m.AppendAssistant(llm.Message{Role: llm.RoleAssistant, Content: "ok"})
	}
	if m.CompactIfNeed() == nil {
		t.Fatal("should compact over threshold")
	}
}

func TestBuildStructuredSummary(t *testing.T) {
	entries := []entry{
		{msg: llm.Message{Role: llm.RoleUser, Content: "read go.mod please"}},
		{
			msg: llm.Message{
				Role:      llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{{Name: "read_file"}},
			},
		},
		{msg: llm.Message{Role: llm.RoleTool, Content: "1| module example.com/app\n"}},
		{msg: llm.Message{Role: llm.RoleAssistant, Content: "The module is example.com/app"}},
	}
	s := BuildStructuredSummary(entries)
	if !strings.Contains(s, "read go.mod") {
		t.Fatalf("missing goal: %s", s)
	}
	if !strings.Contains(s, "read_file") {
		t.Fatalf("missing tool: %s", s)
	}
}

func TestCompactDisabled(t *testing.T) {
	m := New("S", "", 1000)
	m.SetCompressAt(0)
	for i := 0; i < 20; i++ {
		m.AppendUser("x")
	}
	if m.CompactIfNeed() != nil {
		t.Fatal("disabled should not compact")
	}
}
