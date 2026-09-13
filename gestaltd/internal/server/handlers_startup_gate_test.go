package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
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
	defer func() { _ = resp.Body.Close() }()
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

func TestStartupGateReportIncludesDeploymentCoordinationState(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	srv := newTestServer(t, func(cfg *server.Config) {
		ctx := context.Background()
		if _, err := cfg.Services.GestaltdSourceVersionState.Activate(
			ctx,
			"source-current",
			start,
			false,
			appregistry.DefaultRolloutEnrollmentWindow,
			appregistry.DefaultRolloutTimeout,
		); err != nil {
			t.Fatalf("seed source version: %v", err)
		}
		if _, err := cfg.Services.AppRollouts.Create(ctx, &core.AppRollout{
			App: "g-issues", Version: "v2", State: core.AppRolloutStateEnrolling,
			TargetSourceVersion: "source-current", CreatedAt: start,
			EnrollmentEndsAt: start.Add(time.Minute), Deadline: start.Add(10 * time.Minute),
		}); err != nil {
			t.Fatalf("seed app rollout: %v", err)
		}
	})
	testutil.CloseOnCleanup(t, srv)

	resp, err := http.Get(srv.URL + "/startup-gate")
	if err != nil {
		t.Fatalf("GET /startup-gate: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var report map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode startup gate report: %v", err)
	}
	if report["registry_source_version"] != "source-current" {
		t.Fatalf("registry_source_version = %#v", report["registry_source_version"])
	}
	if report["active_app_rollouts"] != float64(1) || report["app_rollouts_settled"] != false {
		t.Fatalf("rollout status = %#v", report)
	}
}
