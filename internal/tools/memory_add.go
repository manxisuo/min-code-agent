package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// MemoryStore is the subset of memory persistence MemoryAdd needs.
type MemoryStore interface {
	Add(entry string) (string, error)
	Compose() string
}

// MemoryAdd appends one durable project fact to memory.md.
type MemoryAdd struct {
	Store MemoryStore
	// OnAdded is optional; called after a successful append (CLI refreshes context / events).
	OnAdded func(entry, composed string)
}

func (t *MemoryAdd) Name() string { return "memory_add" }

func (t *MemoryAdd) Description() string {
	return `Append one durable project fact to memory.md for future sessions.

Use ONLY when the fact will still matter in a NEW chat (stable constraints, long-term decisions, enduring architecture facts). Do NOT store temporary task progress, ephemeral tool output, secrets/API keys, or anything that only applies to this conversation.

Prefer AGENTS.md-style rules over memory when the user states a standing "always/never" policy — memory is for facts, not process rules.

Write one short bullet (one sentence). After adding, do not repeat the fact in your final answer unless asked.`
}

func (t *MemoryAdd) Schema() JSONSchema {
	return JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"entry": map[string]any{
				"type":        "string",
				"description": "One durable fact, one sentence. Example: CLI entry is cmd/mincode/main.go",
			},
		},
		"required": []string{"entry"},
	}
}

type memoryAddArgs struct {
	Entry string `json:"entry"`
}

func (t *MemoryAdd) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if t.Store == nil {
		return Result{Content: "memory store not configured", IsError: true}, nil
	}
	var args memoryAddArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	entry := strings.TrimSpace(args.Entry)
	if entry == "" {
		return Result{Content: "entry is required", IsError: true}, nil
	}
	if looksLikeSecret(entry) {
		return Result{
			Content: "refusing to store a possible secret/credential in memory.md; redact tokens, keys, and passwords",
			IsError: true,
		}, nil
	}

	content, err := t.Store.Add(entry)
	if err != nil {
		return Result{Content: "memory add: " + err.Error(), IsError: true}, nil
	}
	composed := t.Store.Compose()
	if t.OnAdded != nil {
		t.OnAdded(entry, composed)
	}
	return Result{
		Content: fmt.Sprintf("saved to memory.md: %s", entry),
		Meta: map[string]any{
			"path":    "memory.md",
			"entry":   entry,
			"bytes":   len(content),
			"entries": countBullets(content),
		},
	}, nil
}

func countBullets(content string) int {
	n := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			n++
		}
	}
	return n
}

// looksLikeSecret is a cheap heuristic; not a security boundary.
func looksLikeSecret(s string) bool {
	lower := strings.ToLower(s)
	markers := []string{
		"api_key", "api key", "apikey", "secret key", "password=", "password:",
		"token=", "token:", "authorization:", "bearer ", "private_key", "-----begin",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
