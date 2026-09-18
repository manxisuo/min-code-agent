package cli

import (
	"github.com/manxisuo/mincode/internal/ctxmgr"
	"github.com/manxisuo/mincode/internal/session"
)

// sessionExportMeta is header info for a markdown export.
type sessionExportMeta = session.ExportMeta

func renderSessionMarkdown(meta sessionExportMeta, entries []ctxmgr.ExportedEntry) string {
	return session.RenderMarkdown(meta, entries)
}

func defaultExportPath(workspace, sessionID string) string {
	return session.DefaultExportPath(workspace, sessionID)
}

func writeExportFile(path, content string) error {
	return session.WriteExportFile(path, content)
}
