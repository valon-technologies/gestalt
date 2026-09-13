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
	RegistrySourceVersion        string `json:"registry_source_version,omitempty"`
	ActiveAppRollouts            int    `json:"active_app_rollouts"`
	AppRolloutsSettled           bool   `json:"app_rollouts_settled"`
	AppDeployPauseOwner          string `json:"app_deploy_pause_owner,omitempty"`
}

func (s *Server) startupGateReport(w http.ResponseWriter, r *http.Request) {
	report := startupGateReport{
		PromoteSharedStateOnActivate: s.promoteSharedStateOnActivate,
		RejectSharedStatePromotion:   s.rejectSharedStatePromotion,
		SourceVersion:                s.sourceVersion,
		ReadyProbePath:               "/ready",
	}
	if s.gestaltdSourceVersions != nil && s.appRollouts != nil {
		state, currentErr := s.gestaltdSourceVersions.Get(r.Context())
		active, activeErr := s.appRollouts.ListActive(r.Context())
		if currentErr == nil && activeErr == nil {
			report.RegistrySourceVersion = state.CurrentSourceVersion
			report.AppDeployPauseOwner = state.AppDeployPauseOwner
			report.ActiveAppRollouts = len(active)
			report.AppRolloutsSettled = len(active) == 0
		}
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
