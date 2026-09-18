package server

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/manxisuo/mincode/internal/session"
)

func (s *Server) renderExportMarkdown() (string, string) {
	if s.agent == nil || s.agent.Ctx == nil {
		return "", ""
	}
	id := s.activeSessionID()
	meta := session.ExportMeta{
		SessionID: id,
		Workspace: s.opts.Workspace,
		Provider:  s.opts.Provider,
		Model:     s.opts.Model,
		CreatedAt: time.Now().UTC(),
	}
	md := session.RenderMarkdown(meta, s.agent.Ctx.ExportEntries())
	path := session.DefaultExportPath(s.opts.Workspace, id)
	return md, path
}

// POST /api/export — write exports/<session>.md under the workspace.
func (s *Server) handleExportWrite(w http.ResponseWriter, _ *http.Request) {
	md, path := s.renderExportMarkdown()
	if md == "" {
		writeErr(w, http.StatusNotFound, "no agent context to export")
		return
	}
	if err := session.WriteExportFile(path, md); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	rel, err := filepath.Rel(s.opts.Workspace, path)
	if err != nil {
		rel = path
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"path":    path,
		"rel":     filepath.ToSlash(rel),
		"bytes":   len(md),
		"session": s.activeSessionID(),
	})
}

// GET /api/export — markdown body (same as CLI /export content).
func (s *Server) handleExportGet(w http.ResponseWriter, _ *http.Request) {
	md, path := s.renderExportMarkdown()
	if md == "" {
		writeErr(w, http.StatusNotFound, "no agent context to export")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"path":     path,
		"markdown": md,
		"bytes":    len(md),
		"session":  s.activeSessionID(),
	})
}

// GET /api/export/download — browser download as .md
func (s *Server) handleExportDownload(w http.ResponseWriter, _ *http.Request) {
	md, _ := s.renderExportMarkdown()
	if md == "" {
		writeErr(w, http.StatusNotFound, "no agent context to export")
		return
	}
	name := s.activeSessionID() + ".md"
	if name == ".md" {
		name = "mincode-session.md"
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(md))
}
