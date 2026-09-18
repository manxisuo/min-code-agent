package server

import (
	"net/http"

	"github.com/manxisuo/mincode/internal/experiment"
)

func (s *Server) handleExperimentList(w http.ResponseWriter, _ *http.Request) {
	if s.experiments == nil {
		writeJSON(w, http.StatusOK, map[string]any{"experiments": []any{}})
		return
	}
	names, err := s.experiments.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	list := make([]experiment.Aggregate, 0, len(names))
	for _, n := range names {
		runs, err := s.experiments.LoadExperiment(n)
		if err != nil {
			continue
		}
		list = append(list, experiment.Summarize(n, runs))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"root":         s.experiments.Root(),
		"experiments":  list,
	})
}

func (s *Server) handleExperimentShow(w http.ResponseWriter, r *http.Request) {
	if s.experiments == nil {
		writeErr(w, http.StatusNotFound, "experiments unavailable")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeErr(w, http.StatusBadRequest, "experiment name required")
		return
	}
	runs, err := s.experiments.LoadExperiment(name)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	agg := experiment.Summarize(name, runs)
	writeJSON(w, http.StatusOK, map[string]any{
		"aggregate": agg,
		"runs":      runs,
	})
}
