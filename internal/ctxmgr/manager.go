package ctxmgr

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/mincode/mincode/internal/llm"
)

// DefaultBudgetTokens is used when config does not set a budget.
const DefaultBudgetTokens = 32000

// maxToolResultTokens caps a single tool result stored in conversation history.
// Prevents one huge read_file from dominating every subsequent LLM call.
const maxToolResultTokens = 2500

type entry struct {
	msg    llm.Message
	source Source
	tokens int
}

// Manager owns conversation state and builds budgeted LLM requests.
type Manager struct {
	system       string
	instructions string
	budget       int
	entries      []entry
	step         int
	lastSnapshot *Snapshot
	cal          *Calibrator
}

// New creates a context manager.
func New(systemPrompt, instructions string, budget int) *Manager {
	if budget <= 0 {
		budget = DefaultBudgetTokens
	}
	return &Manager{
		system:       systemPrompt,
		instructions: instructions,
		budget:       budget,
		cal:          NewCalibrator(),
	}
}

func (m *Manager) Budget() int { return m.budget }

func (m *Manager) SetBudget(n int) {
	if n > 0 {
		m.budget = n
	}
}

func (m *Manager) SetInstructions(s string) { m.instructions = s }
func (m *Manager) Instructions() string     { return m.instructions }
func (m *Manager) System() string           { return m.system }
func (m *Manager) SetSystem(s string)       { m.system = s }
func (m *Manager) Len() int                 { return len(m.entries) }
func (m *Manager) LastSnapshot() *Snapshot  { return m.lastSnapshot }

// Calibrator exposes the live token-ratio calibrator.
func (m *Manager) Calibrator() *Calibrator { return m.cal }

// est scales the base heuristic by the learned provider ratio.
func (m *Manager) est(s string) int {
	return m.cal.Scale(EstimateTokens(s))
}

// ObserveUsage folds provider-reported prompt_tokens into the calibrator.
// estimated should be snap.TotalTokens + snap.ToolTokens from the same call.
func (m *Manager) ObserveUsage(estimated, actualPromptTokens int) {
	m.cal.Observe(estimated, actualPromptTokens)
	if m.lastSnapshot != nil && actualPromptTokens > 0 {
		m.lastSnapshot.ActualPromptTokens = actualPromptTokens
		m.lastSnapshot.EstimateRatio = m.cal.Ratio()
	}
}

// AppendUser records a user message.
func (m *Manager) AppendUser(text string) {
	m.entries = append(m.entries, entry{
		msg:    llm.Message{Role: llm.RoleUser, Content: text},
		source: SourceUserInput,
		tokens: m.est(text),
	})
}

// AppendAssistant records an assistant message (may include tool calls).
func (m *Manager) AppendAssistant(msg llm.Message) {
	content := msg.Content
	for _, tc := range msg.ToolCalls {
		content += "\n" + tc.Name + tc.Arguments
	}
	m.entries = append(m.entries, entry{
		msg:    msg,
		source: SourceHistory,
		tokens: m.est(content),
	})
}

// AppendToolResult records a tool observation tied to a tool_call_id.
// Oversized results are truncated up-front so they cannot flood later turns.
func (m *Manager) AppendToolResult(toolCallID, content string) {
	if m.est(content) > maxToolResultTokens {
		content = m.truncateToTokens(content, maxToolResultTokens)
	}
	m.entries = append(m.entries, entry{
		msg: llm.Message{
			Role:       llm.RoleTool,
			Content:    content,
			ToolCallID: toolCallID,
		},
		source: SourceToolResult,
		tokens: m.est(content),
	})
}

// Clear drops conversation entries (keeps system/instructions).
func (m *Manager) Clear() { m.entries = nil }

// DropLastUser removes a trailing user message (used when a turn fails).
func (m *Manager) DropLastUser() {
	if n := len(m.entries); n > 0 && m.entries[n-1].msg.Role == llm.RoleUser {
		m.entries = m.entries[:n-1]
	}
}

type part struct {
	msg       llm.Message
	src       Source
	tok       int
	pin       bool
	order     int // higher = more recent / more important
	excluded  bool
	truncated bool
}

// BuildRequest assembles a budgeted ChatRequest and records a Snapshot.
// Call AppendUser first for the current turn.
func (m *Manager) BuildRequest(tools []llm.ToolDefinition) (llm.ChatRequest, Snapshot) {
	m.step++
	now := time.Now().UTC()

	toolTok := m.estimateToolDefTokens(tools)
	// Message budget must leave room for tool schemas that ride along every call.
	msgBudget := m.budget - toolTok
	if msgBudget < m.budget/5 {
		msgBudget = m.budget / 5
		if msgBudget < 256 {
			msgBudget = 256
		}
	}

	var parts []part

	if m.system != "" {
		parts = append(parts, part{
			msg:   llm.Message{Role: llm.RoleSystem, Content: m.system},
			src:   SourceSystem,
			tok:   m.est(m.system),
			pin:   true,
			order: 10000,
		})
	}
	if trimSpace(m.instructions) != "" {
		parts = append(parts, part{
			msg:   llm.Message{Role: llm.RoleSystem, Content: m.instructions},
			src:   SourceInstructions,
			tok:   m.est(m.instructions),
			pin:   true,
			order: 9000,
		})
	}
	for i, e := range m.entries {
		src := e.source
		if e.msg.Role == llm.RoleTool {
			src = SourceToolResult
		} else if e.msg.Role == llm.RoleUser {
			if i == len(m.entries)-1 {
				src = SourceUserInput
			} else {
				src = SourceHistory
			}
		} else {
			src = SourceHistory
		}
		parts = append(parts, part{
			msg:   e.msg,
			src:   src,
			tok:   e.tokens,
			order: i + 1,
		})
	}

	total := 0
	for _, p := range parts {
		total += p.tok
	}

	if total > msgBudget {
		idxs := make([]int, 0, len(parts))
		for i, p := range parts {
			if p.pin {
				continue
			}
			idxs = append(idxs, i)
		}
		for a := 0; a < len(idxs); a++ {
			for b := a + 1; b < len(idxs); b++ {
				if parts[idxs[b]].order < parts[idxs[a]].order {
					idxs[a], idxs[b] = idxs[b], idxs[a]
				}
			}
		}
		newest := -1
		for _, p := range parts {
			if p.order > newest {
				newest = p.order
			}
		}
		for _, idx := range idxs {
			if total <= msgBudget {
				break
			}
			if parts[idx].src == SourceUserInput && parts[idx].order == newest {
				continue
			}
			total -= parts[idx].tok
			parts[idx].excluded = true
		}
	}

	if total > msgBudget {
		for i := range parts {
			if total <= msgBudget {
				break
			}
			if parts[i].excluded || parts[i].src != SourceToolResult || parts[i].tok < 200 {
				continue
			}
			need := total - msgBudget
			maxTok := parts[i].tok - need
			if maxTok < 100 {
				maxTok = 100
			}
			newContent := m.truncateToTokens(parts[i].msg.Content, maxTok)
			newTok := m.est(newContent)
			saved := parts[i].tok - newTok
			if saved <= 0 {
				continue
			}
			parts[i].msg.Content = newContent
			parts[i].tok = newTok
			parts[i].truncated = true
			total -= saved
		}
	}

	var (
		messages   []llm.Message
		items      []Item
		systemText string
	)
	finalTotal := 0
	included, excludedN, truncatedN := 0, 0, 0

	for _, p := range parts {
		it := Item{
			Source:     p.src,
			Role:       string(p.msg.Role),
			Preview:    previewOf(p.msg.Content),
			Tokens:     p.tok,
			ToolCallID: p.msg.ToolCallID,
			Pinned:     p.pin,
		}
		if p.excluded {
			it.Included = false
			it.Excluded = true
			it.Reason = "token budget"
			it.Tokens = 0
			items = append(items, it)
			excludedN++
			continue
		}
		if p.truncated {
			it.Truncated = true
			it.Reason = "tool result budget limit"
			truncatedN++
		}
		it.Included = true
		items = append(items, it)
		finalTotal += p.tok
		included++

		if p.msg.Role == llm.RoleSystem {
			if systemText != "" {
				systemText += "\n\n"
			}
			systemText += p.msg.Content
			continue
		}
		messages = append(messages, p.msg)
	}
	if systemText != "" {
		messages = append([]llm.Message{{Role: llm.RoleSystem, Content: systemText}}, messages...)
	}

	snap := Snapshot{
		Step:        m.step,
		Time:        now,
		Items:       items,
		TotalTokens: finalTotal,
		ToolTokens:  toolTok,
		Budget:      m.budget,
		Included:    included,
		Excluded:    excludedN,
		Truncated:   truncatedN,
	}
	m.lastSnapshot = &snap

	return llm.ChatRequest{Messages: messages, Tools: tools}, snap
}

// estimateToolDefTokens estimates the token cost of tool JSON schemas.
// These are sent on every LLM call but are not chat messages.
func (m *Manager) estimateToolDefTokens(defs []llm.ToolDefinition) int {
	if len(defs) == 0 {
		return 0
	}
	total := 0
	for _, d := range defs {
		total += m.est(d.Name)
		total += m.est(d.Description)
		if len(d.Parameters) > 0 {
			total += m.est(string(d.Parameters))
		}
		total += 12 // wire envelope / type markers
	}
	return total
}

func (m *Manager) truncateToTokens(s string, maxTok int) string {
	return truncateToTokensScaled(s, maxTok, m.est)
}

func previewOf(s string) string {
	s = trimSpace(s)
	if s == "" {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

func truncateToTokensScaled(s string, maxTok int, est func(string) int) string {
	if est(s) <= maxTok {
		return s
	}
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return s
	}
	e := est(s)
	keep := n * maxTok / e
	if keep < 20 {
		keep = 20
	}
	if keep > n {
		keep = n
	}
	count := 0
	for i := range s {
		if count == keep {
			return s[:i] + "\n... (truncated, token budget)\n"
		}
		count++
	}
	return s
}

func truncateToTokens(s string, maxTok int) string {
	return truncateToTokensScaled(s, maxTok, EstimateTokens)
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\n' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\n' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// FormatSnapshot renders a snapshot for the CLI.
func FormatSnapshot(s Snapshot) string { return s.Summary() }

func (s Snapshot) String() string {
	return fmt.Sprintf("Snapshot#%d tokens=%d budget=%d items=%d", s.Step, s.TotalTokens, s.Budget, len(s.Items))
}
