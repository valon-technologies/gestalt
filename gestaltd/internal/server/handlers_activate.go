package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func (s *Server) mountActivateRoute(r chi.Router) {
	r.Post("/activate", s.activateAppProvidersHandler)
}

func (s *Server) activateAppProvidersHandler(w http.ResponseWriter, r *http.Request) {
	phase := strings.TrimSpace(r.URL.Query().Get("phase"))
	if phase != "" && phase != "candidate" && phase != "rollback" {
		writeError(w, http.StatusBadRequest, "phase must be candidate or rollback when provided")
		return
	}
	if s.sourceVersion != "" {
		if s.gestaltdSourceVersions == nil {
			writeError(w, http.StatusServiceUnavailable, "gestaltd source version service is unavailable")
			return
		}
		var err error
		switch phase {
		case "candidate":
			_, err = s.gestaltdSourceVersions.BeginPromotion(r.Context(), s.sourceVersion, s.now())
		case "rollback":
			_, err = s.gestaltdSourceVersions.CancelPromotion(r.Context(), s.sourceVersion, s.now())
		default:
			_, err = s.gestaltdSourceVersions.Promote(
				r.Context(),
				s.sourceVersion,
				s.now(),
				appregistry.DefaultRolloutEnrollmentWindow,
				appregistry.DefaultRolloutTimeout,
			)
		}
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, coredata.ErrGestaltdSourceVersionMismatch) {
				status = http.StatusConflict
			}
			writeError(w, status, err.Error())
			return
		}
	}
	if phase != "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": phase})
		return
	}
	if s.activateAppProviders != nil {
		s.activateAppProviders(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
