package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mincode/mincode/internal/instruction"
	"github.com/mincode/mincode/internal/llm"
	"github.com/mincode/mincode/internal/observability"
	"github.com/mincode/mincode/internal/permission"
	"github.com/mincode/mincode/internal/tools"
)

func writeInstrFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstructionLoadedOnToolPath(t *testing.T) {
	root := t.TempDir()
	writeInstrFile(t, filepath.Join(root, "AGENTS.md"), "root rules")
	writeInstrFile(t, filepath.Join(root, "backend", "AGENTS.md"), "backend rules")
	writeInstrFile(t, filepath.Join(root, "backend", "main.go"), "package main\n")

	ws, err := tools.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	reg.Register(&tools.ReadFile{WS: ws})

	loader, err := instruction.NewLoader(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loader.LoadRoot(); err != nil {
		t.Fatal(err)
	}

	bus := observability.NewBus()
	var loadedRel []string
	bus.Subscribe(func(e observability.Event) {
		if e.Type != observability.EventInstructionLoaded {
			return
		}
		switch d := e.Data.(type) {
		case observability.InstructionLoadedData:
			loadedRel = append(loadedRel, d.RelPath)
		case map[string]any:
			if s, ok := d["rel_path"].(string); ok {
				loadedRel = append(loadedRel, s)
			}
		}
	})

	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{
				ID:        "1",
				Name:      "read_file",
				Arguments: `{"path":"backend/main.go"}`,
			}}},
			{Content: "done"},
		},
	}

	ag := New(fake, reg, bus, "sess", 5, "sys", 32000)
	ag.Instr = loader
	ag.Policy = permission.NewDefaultPolicy()
	ag.Ctx.SetInstructions(loader.Compose())

	if _, err := ag.Run(context.Background(), "read backend main"); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ag.Ctx.Instructions(), "backend rules") {
		t.Fatalf("instructions = %q", ag.Ctx.Instructions())
	}
	found := false
	for _, rel := range loadedRel {
		if strings.Contains(rel, "AGENTS.md") && strings.Contains(filepath.ToSlash(rel), "backend") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected instruction.loaded for backend, got %v", loadedRel)
	}
}

func TestInstructionsNotReloadedTwice(t *testing.T) {
	root := t.TempDir()
	writeInstrFile(t, filepath.Join(root, "AGENTS.md"), "root rules")
	writeInstrFile(t, filepath.Join(root, "pkg", "a.go"), "package pkg\n")
	writeInstrFile(t, filepath.Join(root, "pkg", "b.go"), "package pkg\n")

	ws, err := tools.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	reg.Register(&tools.ReadFile{WS: ws})

	loader, err := instruction.NewLoader(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loader.LoadRoot(); err != nil {
		t.Fatal(err)
	}

	bus := observability.NewBus()
	loads := 0
	bus.Subscribe(func(e observability.Event) {
		if e.Type == observability.EventInstructionLoaded {
			loads++
		}
	})

	fake := &llm.FakeProvider{
		Responses: []llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{
				{ID: "1", Name: "read_file", Arguments: `{"path":"pkg/a.go"}`},
				{ID: "2", Name: "read_file", Arguments: `{"path":"pkg/b.go"}`},
			}},
			{Content: "done"},
		},
	}

	ag := New(fake, reg, bus, "sess", 5, "sys", 32000)
	ag.Instr = loader
	ag.Policy = permission.NewDefaultPolicy()
	ag.Ctx.SetInstructions(loader.Compose())

	if _, err := ag.Run(context.Background(), "read pkg files"); err != nil {
		t.Fatal(err)
	}
	// pkg has no AGENTS.md — only root was preloaded; nothing new should load.
	if loads != 0 {
		t.Fatalf("unexpected instruction.loaded events: %d", loads)
	}
	if loader.Count() != 1 {
		t.Fatalf("count = %d", loader.Count())
	}
}
