package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountActivateRoute(r chi.Router) {
	r.Post("/activate", s.activateAppProvidersHandler)
}

func (s *Server) activateAppProvidersHandler(w http.ResponseWriter, r *http.Request) {
	if s.activateAppProviders != nil {
		s.activateAppProviders(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
