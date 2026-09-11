package server

import (
	"net/http"
)

func (s *Server) fleetReadinessReport(w http.ResponseWriter, _ *http.Request) {
	if s.uiReadiness == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"status": "ui readiness disabled"})
		return
	}
	writeJSON(w, http.StatusOK, s.uiReadiness.Report())
}
