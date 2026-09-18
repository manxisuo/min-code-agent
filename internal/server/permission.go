package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/manxisuo/mincode/internal/observability"
	"github.com/manxisuo/mincode/internal/permission"
	"github.com/manxisuo/mincode/internal/tools"
)

// pendingPerm is one Ask-level tool waiting for a web decision.
type pendingPerm struct {
	ID        string    `json:"id"`
	Tool      string    `json:"tool"`
	Arguments string    `json:"arguments"`
	Summary   string    `json:"summary"`
	Diff      string    `json:"diff,omitempty"`
	CreatedAt time.Time `json:"created_at"`

	ch      chan bool
	decided bool
}

// webApprover blocks the agent until the browser Allow/Deny or timeout.
type webApprover struct {
	s       *Server
	timeout time.Duration
}

// WebApprover returns an Approver that pauses tool execution for UI approval.
func (s *Server) WebApprover() permission.Approver {
	return &webApprover{s: s, timeout: 2 * time.Minute}
}

func (a *webApprover) Approve(req permission.Request) (bool, error) {
	id := fmt.Sprintf("perm-%d", time.Now().UnixNano())
	p := &pendingPerm{
		ID:        id,
		Tool:      req.Tool,
		Arguments: req.Arguments,
		Summary:   req.Summary,
		Diff:      a.s.previewDiff(req),
		CreatedAt: time.Now().UTC(),
		ch:        make(chan bool, 1),
	}
	a.s.permMu.Lock()
	if a.s.pendingPerms == nil {
		a.s.pendingPerms = map[string]*pendingPerm{}
	}
	a.s.pendingPerms[id] = p
	a.s.permMu.Unlock()

	a.s.emitPerm(observability.EventPermissionRequested, observability.PermissionData{
		ID:        id,
		Tool:      req.Tool,
		Arguments: req.Arguments,
		Summary:   req.Summary,
		Level:     "ask",
		Diff:      p.Diff,
	})

	timeout := a.timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case ok := <-p.ch:
		return ok, nil
	case <-timer.C:
		a.s.finishPerm(id, false)
		a.s.emitPerm(observability.EventPermissionDenied, observability.PermissionData{
			ID: id, Tool: req.Tool, Summary: req.Summary,
			Level: "ask", Decision: "denied", Reason: "timeout",
		})
		return false, nil
	}
}

func (s *Server) emitPerm(typ observability.EventType, data observability.PermissionData) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(observability.NewEvent(s.opts.SessionID, s.agentCtxLen(), typ, data))
}

func (s *Server) finishPerm(id string, allow bool) bool {
	s.permMu.Lock()
	p, ok := s.pendingPerms[id]
	if ok {
		if p.decided {
			s.permMu.Unlock()
			return false
		}
		p.decided = true
		delete(s.pendingPerms, id)
	}
	s.permMu.Unlock()
	if !ok {
		return false
	}
	p.ch <- allow
	return true
}

func (s *Server) denyAllPending() {
	s.permMu.Lock()
	ids := make([]string, 0, len(s.pendingPerms))
	for id, p := range s.pendingPerms {
		if !p.decided {
			p.decided = true
			ids = append(ids, id)
			p.ch <- false
		}
	}
	for _, id := range ids {
		delete(s.pendingPerms, id)
	}
	s.permMu.Unlock()
	for _, id := range ids {
		s.emitPerm(observability.EventPermissionDenied, observability.PermissionData{
			ID: id, Decision: "denied", Reason: "cancelled",
		})
	}
}

// previewDiff builds a review diff for write/edit (empty otherwise).
func (s *Server) previewDiff(req permission.Request) string {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil || args.Path == "" {
		return ""
	}
	ws := s.ws
	switch req.Tool {
	case "write_file":
		preview := tools.PreviewWrite(args.Path, args.Content)
		if ws == nil {
			return preview
		}
		if abs, err := ws.Resolve(args.Path); err == nil {
			if old, err := os.ReadFile(abs); err == nil {
				return tools.UnifiedDiff(args.Path, string(old), args.Content, 3)
			}
		}
		return preview
	case "edit_file":
		if ws == nil {
			return ""
		}
		d, err := tools.PreviewEdit(ws, args.Path, args.OldText, args.NewText)
		if err != nil {
			return fmt.Sprintf("diff unavailable: %v", err)
		}
		return d
	default:
		// shell / memory: show compact arguments only
		return ""
	}
}

func (s *Server) handlePermissionPending(w http.ResponseWriter, _ *http.Request) {
	s.permMu.Lock()
	list := make([]*pendingPerm, 0, len(s.pendingPerms))
	for _, p := range s.pendingPerms {
		if !p.decided {
			list = append(list, p)
		}
	}
	s.permMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"pending": list})
}

func (s *Server) handlePermissionDecide(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "permission id required")
		return
	}
	var body struct {
		Allow bool `json:"allow"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}

	s.permMu.Lock()
	p := s.pendingPerms[id]
	var summary, tool string
	if p != nil {
		summary, tool = p.Summary, p.Tool
	}
	s.permMu.Unlock()
	if p == nil {
		writeErr(w, http.StatusNotFound, "permission not found")
		return
	}

	if !s.finishPerm(id, body.Allow) {
		writeErr(w, http.StatusConflict, "permission already decided")
		return
	}
	if body.Allow {
		s.emitPerm(observability.EventPermissionApproved, observability.PermissionData{
			ID: id, Tool: tool, Summary: summary, Level: "ask", Decision: "approved",
		})
	} else {
		s.emitPerm(observability.EventPermissionDenied, observability.PermissionData{
			ID: id, Tool: tool, Summary: summary, Level: "ask", Decision: "denied",
			Reason: "user rejected",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "allow": body.Allow})
}
