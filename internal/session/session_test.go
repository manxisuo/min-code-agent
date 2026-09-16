package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mincode/mincode/internal/llm"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	rec := &Record{
		ID:        "sess-1",
		Workspace: "D:/proj",
		Model:     "m",
		Entries: []Entry{
			{Role: "user", Content: "hi", Source: "user_input", Tokens: 2},
			{Role: "assistant", Content: "hello", Source: "history", Tokens: 3},
			{
				Role:      "assistant",
				ToolCalls: []llm.ToolCall{{ID: "c1", Name: "shell", Arguments: "{}"}},
				Source:    "history",
			},
			{Role: "tool", Content: "ok", ToolCallID: "c1", Source: "tool_result"},
		},
	}
	if err := st.Save(rec); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "m" || len(got.Entries) != 4 {
		t.Fatalf("got = %+v", got)
	}
	if got.Entries[2].ToolCalls[0].Name != "shell" {
		t.Fatalf("tool calls lost: %+v", got.Entries[2])
	}
	if got.Entries[3].ToolCallID != "c1" {
		t.Fatal("tool_call_id lost")
	}
}

func TestLatestByWorkspace(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	ws := t.TempDir()

	old := &Record{ID: "old", Workspace: ws, UpdatedAt: time.Now().Add(-time.Hour)}
	newer := &Record{ID: "new", Workspace: ws, UpdatedAt: time.Now()}
	other := &Record{ID: "other", Workspace: t.TempDir(), UpdatedAt: time.Now()}
	for _, r := range []*Record{old, newer, other} {
		if err := st.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.Latest(ws)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "new" {
		t.Fatalf("latest = %s", got.ID)
	}
}

func TestLatestEmpty(t *testing.T) {
	st := NewStore(t.TempDir())
	if _, err := st.Latest("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestListSorted(t *testing.T) {
	st := NewStore(t.TempDir())
	_ = st.Save(&Record{ID: "a", UpdatedAt: time.Now().Add(-2 * time.Hour)})
	_ = st.Save(&Record{ID: "b", UpdatedAt: time.Now()})
	list, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "a" || list[1].ID != "b" {
		t.Fatalf("list = %+v", list)
	}
}

func TestDefaultDir(t *testing.T) {
	p := DefaultDir("/tmp/x")
	if filepath.Base(filepath.Dir(p)) != ".mincode" {
		t.Fatalf("dir = %s", p)
	}
	_ = os.Getenv
}
