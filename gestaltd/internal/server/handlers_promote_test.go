package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestActivateWithoutSharedPromotionLeavesSourceVersionUnchanged(t *testing.T) {
	t.Parallel()

	var services *coredata.Services
	start := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.SourceVersion = "source-new"
		promoteOnActivate := false
		cfg.PromoteSharedStateOnActivate = &promoteOnActivate
		cfg.Now = func() time.Time { return start.Add(5 * time.Minute) }
		services = cfg.Services
		if _, err := services.GestaltdSourceVersionState.Activate(
			context.Background(),
			"source-old",
			start,
			false,
			appregistry.DefaultRolloutEnrollmentWindow,
			appregistry.DefaultRolloutTimeout,
		); err != nil {
			t.Fatalf("seed source version: %v", err)
		}
		if _, err := services.AppRollouts.Create(context.Background(), &core.AppRollout{
			App:                 "g-issues",
			Version:             "v2",
			State:               core.AppRolloutStateEnrolling,
			TargetSourceVersion: "source-old",
			CreatedAt:           start,
			EnrollmentEndsAt:    start.Add(2 * time.Minute),
			Deadline:            start.Add(15 * time.Minute),
		}); err != nil {
			t.Fatalf("seed rollout: %v", err)
		}
	})
	testutil.CloseOnCleanup(t, srv)

	resp, err := http.Post(srv.URL+"/activate?source_version=source-new&minimum_healthy_instances=5", "", nil)
	if err != nil {
		t.Fatalf("POST /activate: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activation status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	current, err := services.GestaltdSourceVersionState.CurrentForAdmission(context.Background())
	if err != nil {
		t.Fatalf("CurrentForAdmission: %v", err)
	}
	if current != "source-old" {
		t.Fatalf("current source version = %q, want source-old", current)
	}
}

func TestPromoteEndpointPromotesSharedState(t *testing.T) {
	t.Parallel()

	var services *coredata.Services
	start := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.SourceVersion = "source-new"
		promoteOnActivate := false
		cfg.PromoteSharedStateOnActivate = &promoteOnActivate
		cfg.Now = func() time.Time { return start.Add(5 * time.Minute) }
		services = cfg.Services
		if _, err := services.GestaltdSourceVersionState.Activate(
			context.Background(),
			"source-old",
			start,
			false,
			appregistry.DefaultRolloutEnrollmentWindow,
			appregistry.DefaultRolloutTimeout,
		); err != nil {
			t.Fatalf("seed source version: %v", err)
		}
		if _, err := services.AppRollouts.Create(context.Background(), &core.AppRollout{
			App:                 "g-issues",
			Version:             "v2",
			State:               core.AppRolloutStateEnrolling,
			TargetSourceVersion: "source-old",
			CreatedAt:           start,
			EnrollmentEndsAt:    start.Add(2 * time.Minute),
			Deadline:            start.Add(15 * time.Minute),
		}); err != nil {
			t.Fatalf("seed rollout: %v", err)
		}
	})
	testutil.CloseOnCleanup(t, srv)

	resp, err := http.Post(srv.URL+"/promote?source_version=source-new&minimum_healthy_instances=5", "", nil)
	if err != nil {
		t.Fatalf("POST /promote: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("promote status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	current, err := services.GestaltdSourceVersionState.CurrentForAdmission(context.Background())
	if err != nil {
		t.Fatalf("CurrentForAdmission: %v", err)
	}
	if current != "source-new" {
		t.Fatalf("current source version = %q, want source-new", current)
	}
}
