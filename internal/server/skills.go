package server

import (
	"net/http"
	"strings"

	"github.com/manxisuo/mincode/internal/observability"
	"github.com/manxisuo/mincode/internal/skill"
	"github.com/manxisuo/mincode/internal/strutil"
)

type skillListItem struct {
	Name    string `json:"name"`
	RelPath string `json:"rel_path"`
	Summary string `json:"summary"`
	Active  bool   `json:"active"`
	Bytes   int    `json:"bytes"`
}

func (s *Server) handleSkillList(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.skills == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"skills": []skillListItem{},
			"dir":    skill.DirName + "/",
			"active": []string{},
			"note":   "skills unavailable",
		})
		return
	}
	// Refresh from disk so newly added SKILL.md files show up.
	if _, err := s.skills.Discover(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	available := s.skills.Available()
	list := make([]skillListItem, 0, len(available))
	activeNames := make([]string, 0, len(available))
	for _, sk := range available {
		act := s.skills.IsActive(sk.Name)
		if act {
			activeNames = append(activeNames, sk.Name)
		}
		list = append(list, skillListItem{
			Name:    sk.Name,
			RelPath: sk.RelPath,
			Summary: strutil.TruncateRunes(sk.Summary, 80),
			Active:  act,
			Bytes:   len(sk.Content),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"skills":        list,
		"dir":           skill.DirName + "/",
		"skills_dir":    s.skills.SkillsDir(),
		"active":        activeNames,
		"active_count":  s.skills.CountActive(),
		"context_chars": len(s.skills.Compose()),
	})
}

func (s *Server) handleSkillShow(w http.ResponseWriter, r *http.Request) {
	if s.skills == nil {
		writeErr(w, http.StatusNotFound, "skills unavailable")
		return
	}
	name := r.PathValue("name")
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		writeErr(w, http.StatusBadRequest, "invalid skill name")
		return
	}
	// Ensure disk is scanned.
	if _, err := s.skills.Discover(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	sk, ok := s.skills.Get(name)
	if !ok || sk == nil {
		writeErr(w, http.StatusNotFound, "skill not found: "+name)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     sk.Name,
		"rel_path": sk.RelPath,
		"path":     sk.Path,
		"summary":  sk.Summary,
		"content":  sk.Content,
		"active":   s.skills.IsActive(sk.Name),
		"bytes":    len(sk.Content),
	})
}

func (s *Server) syncSkillsToAgent() {
	if s.skills == nil || s.agent == nil || s.agent.Ctx == nil {
		return
	}
	s.agent.Ctx.SetSkills(s.skills.Compose())
}

func (s *Server) handleSkillActivate(w http.ResponseWriter, r *http.Request) {
	if s.skills == nil {
		writeErr(w, http.StatusNotFound, "skills unavailable")
		return
	}
	name := r.PathValue("name")
	if name == "" || strings.ContainsAny(name, `/\`) {
		writeErr(w, http.StatusBadRequest, "invalid skill name")
		return
	}
	sk, newly, err := s.skills.Activate(name)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	s.syncSkillsToAgent()
	if newly {
		s.emitSkill(observability.EventSkillLoaded, observability.SkillEventData{
			Name:    sk.Name,
			RelPath: sk.RelPath,
			Bytes:   len(sk.Content),
			Summary: sk.Summary,
			Reason:  "web",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"name":          sk.Name,
		"active":        true,
		"newly":         newly,
		"active_count":  s.skills.CountActive(),
		"context_chars": len(s.skills.Compose()),
	})
}

func (s *Server) handleSkillDeactivate(w http.ResponseWriter, r *http.Request) {
	if s.skills == nil {
		writeErr(w, http.StatusNotFound, "skills unavailable")
		return
	}
	name := r.PathValue("name")
	if name == "" || strings.ContainsAny(name, `/\`) {
		writeErr(w, http.StatusBadRequest, "invalid skill name")
		return
	}
	if !s.skills.Deactivate(name) {
		writeErr(w, http.StatusConflict, "skill is not active: "+name)
		return
	}
	s.syncSkillsToAgent()
	s.emitSkill(observability.EventSkillUnloaded, observability.SkillEventData{
		Name:   name,
		Reason: "web",
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"name":          name,
		"active":        false,
		"active_count":  s.skills.CountActive(),
		"context_chars": len(s.skills.Compose()),
	})
}

func (s *Server) emitSkill(typ observability.EventType, data observability.SkillEventData) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(observability.NewEvent(s.opts.SessionID, s.agentCtxLen(), typ, data))
}
