package cli

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mincode/mincode/internal/llm"
	"github.com/mincode/mincode/internal/memory"
	"github.com/mincode/mincode/internal/observability"
	"github.com/mincode/mincode/internal/permission"
)

const memoryExtractMaxRune = 8000

// maybeExtractMemory runs after a successful turn when memory.auto_extract is on.
// It asks the LLM for one durable fact; if present, requests approval and saves.
func (a *App) maybeExtractMemory(ctx context.Context, userText, final string) {
	if !a.cfg.Memory.AutoExtract || a.mem == nil || a.provider == nil {
		return
	}
	if strings.TrimSpace(final) == "" {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}

	entry, err := a.extractMemoryEntry(ctx, userText, final)
	if err != nil || entry == "" {
		return
	}
	if looksLikeSecretEntry(entry) {
		fmt.Fprintf(a.out, "%s skipped memory extract (possible secret)\n", dim("memory"))
		return
	}

	req := permission.Request{
		Tool:      "memory_add",
		Arguments: fmt.Sprintf(`{"entry":%q}`, entry),
		Summary:   "memory_add " + truncateStr(entry, 72),
	}
	level := permission.Ask
	if a.agent.Policy != nil {
		level = a.agent.Policy.Evaluate(req)
	}
	if level == permission.Deny {
		return
	}
	a.emit(observability.EventPermissionRequested, observability.PermissionData{
		Tool:    "memory_add",
		Summary: req.Summary,
		Level:   level.String(),
	})
	denied, err := permission.Evaluate(a.agent.Policy, a.agent.Approver, req)
	if err != nil || denied {
		a.emit(observability.EventPermissionDenied, observability.PermissionData{
			Tool:     "memory_add",
			Summary:  req.Summary,
			Decision: "denied",
		})
		return
	}
	a.emit(observability.EventPermissionApproved, observability.PermissionData{
		Tool:     "memory_add",
		Summary:  req.Summary,
		Decision: "approved",
	})

	content, err := a.mem.Add(entry)
	if err != nil {
		fmt.Fprintf(a.out, "%s memory extract save: %v\n", yellow("warn"), err)
		return
	}
	a.agent.Ctx.SetMemory(a.mem.Compose())
	a.emit(observability.EventMemoryUpdated, observability.MemoryEventData{
		RelPath: memory.FileName,
		Bytes:   len(content),
		Entries: countMemoryBullets(content),
		Entry:   entry,
		Reason:  "auto_extract",
	})
	fmt.Fprintf(a.out, "%s %s\n", green("memory"), entry)
}

func (a *App) extractMemoryEntry(ctx context.Context, userText, final string) (string, error) {
	user := truncateRunes(strings.TrimSpace(userText), 1500)
	asst := truncateRunes(strings.TrimSpace(final), 2000)
	prompt := `Review this coding-assistant turn. Propose AT MOST one durable project fact for MEMORY.md.

Save only if it will still matter in a NEW chat (stable architecture fact, long-term decision, enduring constraint).
Do NOT save: temporary task progress, secrets, session-specific chatter, or generic advice.
If nothing qualifies, reply with exactly: NONE
If something qualifies, reply with ONE short sentence only (no bullets, no quotes, no preamble).

USER:
` + user + `

ASSISTANT:
` + asst

	req := llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: truncateRunes(prompt, memoryExtractMaxRune)},
		},
	}
	resp, err := a.provider.Chat(ctx, req)
	if err != nil {
		return "", err
	}
	entry := strings.TrimSpace(resp.Content)
	entry = strings.Trim(entry, "\"'`")
	if entry == "" || strings.EqualFold(entry, "none") || strings.EqualFold(entry, "none.") {
		return "", nil
	}
	if strings.Contains(entry, "\n") {
		entry = strings.TrimSpace(strings.Split(entry, "\n")[0])
	}
	return truncateRunes(entry, 200), nil
}

func looksLikeSecretEntry(s string) bool {
	lower := strings.ToLower(s)
	for _, m := range []string{
		"api_key", "api key", "apikey", "password", "secret key",
		"token=", "token:", "bearer ", "private_key", "-----begin",
	} {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	n := utf8.RuneCountInString(s)
	if n <= max {
		return s
	}
	i := 0
	for idx := range s {
		if i == max {
			return s[:idx] + "…"
		}
		i++
	}
	return s
}
