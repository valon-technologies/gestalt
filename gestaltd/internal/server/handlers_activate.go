package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountActivateRoute(r chi.Router) {
	r.Post("/activate", s.activateAppProvidersHandler)
}

func (s *Server) activateAppProvidersHandler(w http.ResponseWriter, r *http.Request) {
	req, errMsg := parseSharedActivationRequest(r.URL.Query())
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if s.promoteSharedStateOnActivate {
		if !s.promoteFleetSourceVersion(w, r, req) || !s.commitDeferredStartupState(w, r) {
			return
		}
	}
	if !s.runLocalActivation(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
