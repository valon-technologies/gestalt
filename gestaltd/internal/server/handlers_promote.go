package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountPromoteRoute(r chi.Router) {
	r.Post("/promote", s.promoteSharedStateHandler)
}

func (s *Server) promoteSharedStateHandler(w http.ResponseWriter, r *http.Request) {
	req, errMsg := parseSharedActivationRequest(r.URL.Query())
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if !s.promoteFleetSourceVersion(w, r, req) || !s.commitDeferredStartupState(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
