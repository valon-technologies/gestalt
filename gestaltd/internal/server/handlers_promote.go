package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountPromoteRoute(r chi.Router) {
	r.Post("/promote", s.promoteSharedStateHandler)
	r.Post("/promote/temporal", s.promoteTemporalHandler)
	r.Post("/promote/registry", s.promoteRegistryHandler)
}

func (s *Server) promoteSharedStateHandler(w http.ResponseWriter, r *http.Request) {
	req, errMsg := parseSharedActivationRequest(r.URL.Query())
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if !s.promoteTemporalWorkers(w, r) || !s.promoteFleetSourceVersion(w, r, req) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) promoteTemporalHandler(w http.ResponseWriter, r *http.Request) {
	if !s.promoteTemporalWorkers(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "temporal_promoted"})
}

func (s *Server) promoteRegistryHandler(w http.ResponseWriter, r *http.Request) {
	req, errMsg := parseSharedActivationRequest(r.URL.Query())
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if !s.requireTemporalWorkersPromoted(w) {
		return
	}
	if !s.promoteFleetSourceVersion(w, r, req) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "registry_promoted"})
}

func (s *Server) promoteTemporalWorkers(w http.ResponseWriter, r *http.Request) bool {
	if s.finishSharedStartupPromotion == nil {
		return true
	}
	if err := s.finishSharedStartupPromotion(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}

func (s *Server) requireTemporalWorkersPromoted(w http.ResponseWriter) bool {
	if s.temporalWorkersPromoted == nil {
		return true
	}
	if s.temporalWorkersPromoted() {
		return true
	}
	writeError(w, http.StatusPreconditionFailed, "temporal workers must be promoted before registry coordination")
	return false
}
