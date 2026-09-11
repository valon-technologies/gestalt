package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestStartupGateReportReflectsRuntimeSettings(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.SourceVersion = "source-new"
		promoteOnActivate := false
		cfg.PromoteSharedStateOnActivate = &promoteOnActivate
	})
	testutil.CloseOnCleanup(t, srv)

	resp, err := http.Get(srv.URL + "/startup-gate")
	if err != nil {
		t.Fatalf("GET /startup-gate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var report map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode startup gate report: %v", err)
	}
	if report["ui_readiness_enabled"] != false {
		t.Fatalf("ui_readiness_enabled = %#v, want false", report["ui_readiness_enabled"])
	}
	if report["promote_shared_state_on_activate"] != false {
		t.Fatalf("promote_shared_state_on_activate = %#v, want false", report["promote_shared_state_on_activate"])
	}
	if report["source_version"] != "source-new" {
		t.Fatalf("source_version = %#v, want source-new", report["source_version"])
	}
	if report["ready_probe_path"] != "/ready" {
		t.Fatalf("ready_probe_path = %#v, want /ready", report["ready_probe_path"])
	}
}
