package server

import (
	"net/http"
	"os"

	"github.com/manxisuo/mincode/internal/instruction"
	"github.com/manxisuo/mincode/internal/observability"
	"github.com/manxisuo/mincode/internal/strutil"
)

type instructionListItem struct {
	RelPath string `json:"rel_path"`
	RelDir  string `json:"rel_dir"`
	Path    string `json:"path"`
	Bytes   int    `json:"bytes"`
	Preview string `json:"preview"`
	Content string `json:"content,omitempty"`
}

func (s *Server) handleInstructionList(w http.ResponseWriter, r *http.Request) {
	if s.instr == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"instructions": []instructionListItem{},
			"file_name":    instruction.FileName,
			"note":         "instructions unavailable",
		})
		return
	}
	// Ensure root is present (idempotent).
	if _, err := s.instr.LoadRoot(); err != nil && !isNotExist(err) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncInstructionsToAgent()

	files := s.instr.Files()
	withContent := r.URL.Query().Get("content") == "1"
	list := make([]instructionListItem, 0, len(files))
	for _, f := range files {
		item := instructionListItem{
			RelPath: f.RelPath,
			RelDir:  f.RelDir,
			Path:    f.Path,
			Bytes:   len(f.Content),
			Preview: strutil.TruncateRunes(firstLines(f.Content, 4), 160),
		}
		if withContent {
			item.Content = f.Content
		}
		list = append(list, item)
	}

	composed := ""
	if s.agent != nil && s.agent.Ctx != nil {
		composed = s.agent.Ctx.Instructions()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instructions":   list,
		"file_name":      instruction.FileName,
		"workspace":      s.opts.Workspace,
		"count":          len(list),
		"composed_chars": len(composed),
		"composed":       composed,
	})
}

func (s *Server) handleInstructionShow(w http.ResponseWriter, _ *http.Request) {
	if s.instr == nil {
		writeErr(w, http.StatusNotFound, "instructions unavailable")
		return
	}
	if _, err := s.instr.LoadRoot(); err != nil && !isNotExist(err) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	files := s.instr.Files()
	out := make([]instructionListItem, 0, len(files))
	for _, f := range files {
		out = append(out, instructionListItem{
			RelPath: f.RelPath,
			RelDir:  f.RelDir,
			Path:    f.Path,
			Bytes:   len(f.Content),
			Preview: strutil.TruncateRunes(firstLines(f.Content, 4), 160),
			Content: f.Content,
		})
	}
	composed := ""
	if s.agent != nil && s.agent.Ctx != nil {
		composed = s.agent.Ctx.Instructions()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instructions":   out,
		"file_name":      instruction.FileName,
		"count":          len(out),
		"composed_chars": len(composed),
	})
}

// handleInstructionReload re-scans workspace/AGENTS.md and refreshes context.
func (s *Server) handleInstructionReload(w http.ResponseWriter, _ *http.Request) {
	if s.instr == nil {
		writeErr(w, http.StatusNotFound, "instructions unavailable")
		return
	}
	f, err := s.instr.LoadRoot()
	if err != nil && !isNotExist(err) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncInstructionsToAgent()
	if f != nil {
		s.emitInstr(f)
	}
	files := s.instr.Files()
	composed := ""
	if s.agent != nil && s.agent.Ctx != nil {
		composed = s.agent.Ctx.Instructions()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"count":          len(files),
		"composed_chars": len(composed),
		"root":           f != nil,
	})
}

func (s *Server) syncInstructionsToAgent() {
	if s.instr == nil || s.agent == nil || s.agent.Ctx == nil {
		return
	}
	s.agent.Ctx.SetInstructions(s.instr.Compose())
}

func (s *Server) emitInstr(f *instruction.File) {
	if s.bus == nil || f == nil {
		return
	}
	s.bus.Publish(observability.NewEvent(s.opts.SessionID, s.agentCtxLen(),
		observability.EventInstructionLoaded,
		observability.InstructionLoadedData{
			Path:    f.Path,
			RelPath: f.RelPath,
			RelDir:  f.RelDir,
			Bytes:   len(f.Content),
		}))
}

func firstLines(s string, n int) string {
	if s == "" {
		return ""
	}
	lines := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines++
			if lines >= n {
				return s[:i]
			}
		}
	}
	return s
}

func isNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
