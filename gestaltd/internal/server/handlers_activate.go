package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func (s *Server) mountActivateRoute(r chi.Router) {
	r.Post("/activate", s.activateAppProvidersHandler)
}

func (s *Server) activateAppProvidersHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	retryParam := strings.TrimSpace(query.Get("retry"))
	if retryParam != "" && retryParam != "true" {
		writeError(w, http.StatusBadRequest, "retry must be true when provided")
		return
	}
	minimumHealthyInstances := 0
	if query.Has("minimum_healthy_instances") {
		values := query["minimum_healthy_instances"]
		if len(values) != 1 {
			writeError(w, http.StatusBadRequest, "minimum_healthy_instances must be provided once")
			return
		}
		var err error
		minimumHealthyInstances, err = strconv.Atoi(strings.TrimSpace(values[0]))
		if err != nil || minimumHealthyInstances <= 0 {
			writeError(w, http.StatusBadRequest, "minimum_healthy_instances must be a positive integer")
			return
		}
	}
	if s.sourceVersion != "" {
		expectedSourceVersion := strings.TrimSpace(query.Get("source_version"))
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
			minimumHealthyInstances,
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
