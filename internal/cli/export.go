package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manxisuo/mincode/internal/ctxmgr"
	"github.com/manxisuo/mincode/internal/llm"
)

const exportToolResultMax = 2000

// sessionExportMeta is header info for a markdown export.
type sessionExportMeta struct {
	SessionID string
	Workspace string
	Provider  string
	Model     string
	CreatedAt time.Time
}

// renderSessionMarkdown turns conversation entries into a readable Markdown document.
func renderSessionMarkdown(meta sessionExportMeta, entries []ctxmgr.ExportedEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Session %s\n\n", meta.SessionID)
	b.WriteString("| Field | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Workspace | `%s` |\n", meta.Workspace)
	fmt.Fprintf(&b, "| Provider | %s |\n", meta.Provider)
	fmt.Fprintf(&b, "| Model | %s |\n", meta.Model)
	if !meta.CreatedAt.IsZero() {
		fmt.Fprintf(&b, "| Started | %s |\n", meta.CreatedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "| Exported | %s |\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "| Messages | %d |\n\n", len(entries))

	if len(entries) == 0 {
		b.WriteString("_No conversation yet._\n")
		return b.String()
	}

	b.WriteString("## Conversation\n\n")
	for _, e := range entries {
		writeExportEntry(&b, e)
	}
	return b.String()
}

func writeExportEntry(b *strings.Builder, e ctxmgr.ExportedEntry) {
	msg := e.Msg
	switch msg.Role {
	case llm.RoleUser:
		b.WriteString("### User\n\n")
		writeMarkdownBody(b, msg.Content)
		b.WriteString("\n")
	case llm.RoleAssistant:
		b.WriteString("### Assistant\n\n")
		if strings.TrimSpace(msg.Content) != "" {
			writeMarkdownBody(b, msg.Content)
			b.WriteString("\n")
		}
		if len(msg.ToolCalls) > 0 {
			b.WriteString("#### Tool calls\n\n")
			for _, tc := range msg.ToolCalls {
				args := compactExportJSON(tc.Arguments)
				fmt.Fprintf(b, "- `%s` %s\n", tc.Name, grayCode(args))
			}
			b.WriteString("\n")
		}
	case llm.RoleTool:
		b.WriteString("#### Tool result")
		if msg.ToolCallID != "" {
			fmt.Fprintf(b, " (`%s`)", msg.ToolCallID)
		}
		b.WriteString("\n\n```\n")
		body := msg.Content
		if len(body) > exportToolResultMax {
			body = body[:exportToolResultMax] + "\n… (truncated)\n"
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n\n")
	default:
		if strings.TrimSpace(msg.Content) != "" {
			fmt.Fprintf(b, "### %s\n\n", msg.Role)
			writeMarkdownBody(b, msg.Content)
			b.WriteString("\n")
		}
	}
}

func writeMarkdownBody(b *strings.Builder, content string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	// Fence content that already looks like markdown-heavy so it stays readable.
	if strings.Contains(content, "```") {
		b.WriteString(content)
		b.WriteString("\n")
		return
	}
	b.WriteString(content)
	b.WriteString("\n")
}

func grayCode(s string) string {
	if s == "" {
		return ""
	}
	return "`" + strings.ReplaceAll(s, "`", "'") + "`"
}

func compactExportJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

// defaultExportPath is workspace/exports/<session-id>.md
func defaultExportPath(workspace, sessionID string) string {
	return filepath.Join(workspace, "exports", sessionID+".md")
}

func writeExportFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
