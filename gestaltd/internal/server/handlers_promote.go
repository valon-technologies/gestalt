package server

import (
	"context"
	"net/http"
	"strings"

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
	sourceVersion, ok := s.validatePromotionSourceVersion(w, r)
	if !ok {
		return
	}
	if !s.ensureFleetTemporalWorkersPromoted(w, r, sourceVersion) || !s.promoteFleetSourceVersion(w, r, req) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) promoteTemporalHandler(w http.ResponseWriter, r *http.Request) {
	sourceVersion, ok := s.validatePromotionSourceVersion(w, r)
	if !ok {
		return
	}
	if !s.ensureFleetTemporalWorkersPromoted(w, r, sourceVersion) {
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
	sourceVersion, ok := s.validatePromotionSourceVersion(w, r)
	if !ok {
		return
	}
	if !s.requireFleetTemporalWorkersPromoted(w, r, sourceVersion) {
		return
	}
	if !s.promoteFleetSourceVersion(w, r, req) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "registry_promoted"})
}

func (s *Server) validatePromotionSourceVersion(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.sourceVersion == "" {
		return "", true
	}
	expectedSourceVersion := strings.TrimSpace(firstQueryValue(r.URL.Query(), "source_version"))
	if expectedSourceVersion == "" {
		writeError(w, http.StatusBadRequest, "source_version is required")
		return "", false
	}
	if expectedSourceVersion != s.sourceVersion {
		writeError(w, http.StatusConflict, "source_version does not match this gestaltd revision")
		return "", false
	}
	return expectedSourceVersion, true
}

func (s *Server) ensureFleetTemporalWorkersPromoted(w http.ResponseWriter, r *http.Request, sourceVersion string) bool {
	if sourceVersion == "" {
		return s.promoteTemporalWorkers(w, r)
	}
	promoted, err := s.fleetTemporalWorkersPromoted(r.Context(), sourceVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if promoted {
		return s.promoteTemporalWorkers(w, r)
	}
	if !s.promoteTemporalWorkers(w, r) {
		return false
	}
	return s.recordFleetTemporalWorkersPromoted(w, r, sourceVersion)
}

func (s *Server) requireFleetTemporalWorkersPromoted(w http.ResponseWriter, r *http.Request, sourceVersion string) bool {
	if sourceVersion == "" {
		return s.requireTemporalWorkersPromoted(w)
	}
	promoted, err := s.fleetTemporalWorkersPromoted(r.Context(), sourceVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if promoted {
		return true
	}
	writeError(w, http.StatusPreconditionFailed, "temporal workers must be promoted before registry coordination")
	return false
}

func (s *Server) fleetTemporalWorkersPromoted(ctx context.Context, sourceVersion string) (bool, error) {
	if s.gestaltdSourceVersions == nil {
		return false, nil
	}
	return s.gestaltdSourceVersions.TemporalWorkersPromotedForSourceVersion(ctx, sourceVersion)
}

func (s *Server) recordFleetTemporalWorkersPromoted(w http.ResponseWriter, r *http.Request, sourceVersion string) bool {
	if s.gestaltdSourceVersions == nil {
		return true
	}
	_, err := s.gestaltdSourceVersions.MarkTemporalWorkersPromoted(r.Context(), sourceVersion, s.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
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
