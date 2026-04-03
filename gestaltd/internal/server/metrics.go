package server

import (
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
)

func (s *Server) operationMetricsOverview(w http.ResponseWriter, r *http.Request) {
	if s.operationMetrics == nil {
		writeJSON(w, http.StatusOK, core.OperationMetricsOverview{
			Enabled: false,
			Reason:  "built-in metrics are unavailable for the configured telemetry provider",
		})
		return
	}

	overview, err := s.operationMetrics.Overview(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load metrics overview")
		return
	}
	writeJSON(w, http.StatusOK, overview)
}
