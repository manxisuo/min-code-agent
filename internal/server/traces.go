package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/manxisuo/mincode/internal/observability"
)

type traceListItem struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	IsCurrent bool      `json:"is_current"`
	Source    string    `json:"source,omitempty"`
}

func (s *Server) traceSearchDirs() []string {
	dirs := []string{s.traceDir}
	extra := s.opts.TraceDirExtra
	if extra != "" && !strings.EqualFold(filepath.Clean(extra), filepath.Clean(s.traceDir)) {
		dirs = append(dirs, extra)
	}
	return dirs
}

func (s *Server) findTraceFile(id string) (string, bool) {
	if id == "" || strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return "", false
	}
	for _, dir := range s.traceSearchDirs() {
		p := filepath.Join(dir, id+".jsonl")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

func (s *Server) handleTraceList(w http.ResponseWriter, _ *http.Request) {
	type seenKey struct{}
	seen := map[string]seenKey{}
	list := make([]traceListItem, 0, 32)

	for _, dir := range s.traceSearchDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			id := strings.TrimSuffix(e.Name(), ".jsonl")
			if _, ok := seen[id]; ok {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			seen[id] = seenKey{}
			src := "primary"
			if !strings.EqualFold(filepath.Clean(dir), filepath.Clean(s.traceDir)) {
				src = "legacy"
			}
			list = append(list, traceListItem{
				ID:        id,
				Path:      filepath.Join(dir, e.Name()),
				Size:      info.Size(),
				ModTime:   info.ModTime(),
				IsCurrent: id == s.opts.SessionID,
				Source:    src,
			})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ModTime.After(list[j].ModTime)
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"dir":     s.traceDir,
		"dirs":    s.traceSearchDirs(),
		"traces":  list,
		"session": s.opts.SessionID,
	})
}

// handleTraceShow loads one historical trace JSONL.
// Query: type=  (prefix match), limit=N (default 500).
func (s *Server) handleTraceShow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path, ok := s.findTraceFile(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "trace not found: "+id)
		return
	}

	events, err := observability.ReadEvents(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "trace not found: "+id)
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		if n := atoiSafe(v); n > 0 {
			limit = n
		}
	}

	filtered := events
	if typeFilter != "" {
		out := make([]observability.Event, 0, len(events))
		for _, e := range events {
			if strings.HasPrefix(string(e.Type), typeFilter) {
				out = append(out, e)
			}
		}
		filtered = out
	}
	total := len(filtered)
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     id,
		"path":   path,
		"total":  total,
		"shown":  len(filtered),
		"events": filtered,
	})
}

func atoiSafe(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
		if n > 1_000_000 {
			return 1_000_000
		}
	}
	return n
}
