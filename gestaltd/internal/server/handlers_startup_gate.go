package server

import (
	"net/http"
)

type startupGateReport struct {
	UIReadinessEnabled           bool   `json:"ui_readiness_enabled"`
	PromoteSharedStateOnActivate bool   `json:"promote_shared_state_on_activate"`
	RejectSharedStatePromotion   bool   `json:"reject_shared_state_promotion"`
	ReleaseID                    string `json:"release_id"`
	SourceVersion                string `json:"source_version"`
	ReadyProbePath               string `json:"ready_probe_path"`
}

func (s *Server) startupGateReport(w http.ResponseWriter, _ *http.Request) {
	report := startupGateReport{
		PromoteSharedStateOnActivate: s.promoteSharedStateOnActivate,
		RejectSharedStatePromotion:   s.rejectSharedStatePromotion,
		SourceVersion:                s.sourceVersion,
		ReadyProbePath:               "/ready",
	}
	if s.uiReadiness != nil {
		report.UIReadinessEnabled = true
		fleetReport := s.uiReadiness.Report()
		report.ReleaseID = fleetReport.ReleaseID
		if report.SourceVersion == "" {
			report.SourceVersion = fleetReport.SourceVersion
		}
	}
	writeJSON(w, http.StatusOK, report)
}
