package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func (s *Server) mountActivateRoute(r chi.Router) {
	r.Post("/activate", s.activateAppProvidersHandler)
}

func (s *Server) activateAppProvidersHandler(w http.ResponseWriter, r *http.Request) {
	retryParam := strings.TrimSpace(r.URL.Query().Get("retry"))
	if retryParam != "" && retryParam != "true" {
		writeError(w, http.StatusBadRequest, "retry must be true when provided")
		return
	}
	if s.sourceVersion != "" {
		expectedSourceVersion := strings.TrimSpace(r.URL.Query().Get("source_version"))
		if expectedSourceVersion == "" {
			writeError(w, http.StatusBadRequest, "source_version is required")
			return
		}
		if expectedSourceVersion != s.sourceVersion {
			writeError(w, http.StatusConflict, "source_version does not match this gestaltd revision")
			return
		}
		if s.gestaltdSourceVersions == nil {
			writeError(w, http.StatusServiceUnavailable, "gestaltd source version service is unavailable")
			return
		}
		_, err := s.gestaltdSourceVersions.Activate(
			r.Context(),
			s.sourceVersion,
			s.now(),
			retryParam == "true",
			appregistry.DefaultRolloutEnrollmentWindow,
			appregistry.DefaultRolloutTimeout,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if s.activateAppProviders != nil {
		s.activateAppProviders(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
