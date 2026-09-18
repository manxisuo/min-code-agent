package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/manxisuo/mincode/internal/memory"
	"github.com/manxisuo/mincode/internal/observability"
	"github.com/manxisuo/mincode/internal/strutil"
)

// SetMemory wires the workspace MEMORY.md store into the web API.
func (s *Server) SetMemory(m *memory.Store) {
	s.mem = m
	s.syncMemoryToAgent()
}

func (s *Server) syncMemoryToAgent() {
	if s.mem == nil || s.agent == nil || s.agent.Ctx == nil {
		return
	}
	s.agent.Ctx.SetMemory(s.mem.Compose())
}

func (s *Server) handleMemoryGet(w http.ResponseWriter, _ *http.Request) {
	if s.mem == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"file_name": memory.FileName,
			"content":   "",
			"exists":    false,
			"note":      "memory unavailable",
		})
		return
	}
	content, _, _ := s.mem.Load()
	s.syncMemoryToAgent()
	entries := countMemoryBullets(content)
	composed := ""
	if s.agent != nil && s.agent.Ctx != nil {
		composed = s.agent.Ctx.Memory()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"file_name":      memory.FileName,
		"path":           s.mem.Path(),
		"content":        content,
		"exists":         s.mem.Exists(),
		"entries":        entries,
		"bytes":          len(content),
		"composed_chars": len(composed),
		"preview":        strutil.TruncateRunes(content, 200),
	})
}

type memoryAddRequest struct {
	Entry string `json:"entry"`
}

func (s *Server) handleMemoryAdd(w http.ResponseWriter, r *http.Request) {
	if s.mem == nil {
		writeErr(w, http.StatusNotFound, "memory unavailable")
		return
	}
	var req memoryAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	entry := strings.TrimSpace(req.Entry)
	if entry == "" {
		writeErr(w, http.StatusBadRequest, "entry is required")
		return
	}
	content, err := s.mem.Add(entry)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncMemoryToAgent()
	s.bus.Publish(observability.NewEvent(s.opts.SessionID, s.agentCtxLen(),
		observability.EventMemoryUpdated,
		observability.MemoryEventData{
			RelPath: memory.FileName,
			Bytes:   len(content),
			Entry:   entry,
			Reason:  "web",
		}))
	composed := ""
	if s.agent != nil && s.agent.Ctx != nil {
		composed = s.agent.Ctx.Memory()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"content":        content,
		"entries":        countMemoryBullets(content),
		"composed_chars": len(composed),
	})
}

func countMemoryBullets(content string) int {
	n := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			n++
		}
	}
	return n
}
