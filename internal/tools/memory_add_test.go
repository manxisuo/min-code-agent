package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeMem struct {
	entries []string
}

func (f *fakeMem) Add(entry string) (string, error) {
	f.entries = append(f.entries, entry)
	return strings.Join(f.entries, "\n"), nil
}

func (f *fakeMem) Compose() string {
	return "MEM:" + strings.Join(f.entries, ";")
}

func TestMemoryAddSuccess(t *testing.T) {
	fm := &fakeMem{}
	var gotEntry, gotCompose string
	tool := &MemoryAdd{
		Store: fm,
		OnAdded: func(entry, composed string) {
			gotEntry, gotCompose = entry, composed
		},
	}
	if tool.Name() != "memory_add" {
		t.Fatal(tool.Name())
	}
	if !strings.Contains(tool.Description(), "durable") || !strings.Contains(tool.Description(), "Do NOT") {
		t.Fatalf("description should state when to use / not use")
	}

	raw, _ := json.Marshal(map[string]any{"entry": "CLI entry is cmd/mincode"})
	res, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("res = %+v", res)
	}
	if gotEntry != "CLI entry is cmd/mincode" {
		t.Fatalf("OnAdded entry = %q", gotEntry)
	}
	if !strings.HasPrefix(gotCompose, "MEM:") {
		t.Fatalf("compose = %q", gotCompose)
	}
	if res.Meta["path"] != "memory.md" {
		t.Fatalf("meta = %+v", res.Meta)
	}
}

func TestMemoryAddValidation(t *testing.T) {
	tool := &MemoryAdd{Store: &fakeMem{}}
	for _, args := range []string{
		`{}`,
		`{"entry":"   "}`,
		`not-json`,
	} {
		res, err := tool.Execute(context.Background(), json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatalf("args %s should error", args)
		}
	}
}

func TestMemoryAddRejectsSecrets(t *testing.T) {
	fm := &fakeMem{}
	tool := &MemoryAdd{Store: fm}
	raw, _ := json.Marshal(map[string]any{"entry": "api_key=sk-abc123"})
	res, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content, "secret") {
		t.Fatalf("res = %+v", res)
	}
	if len(fm.entries) != 0 {
		t.Fatal("must not write secret")
	}
}

func TestMemoryAddNilStore(t *testing.T) {
	tool := &MemoryAdd{}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"entry":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("nil store must error")
	}
}
