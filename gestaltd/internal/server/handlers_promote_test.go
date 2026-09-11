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

func TestPromoteRegistryRequiresTemporalWorkersFirst(t *testing.T) {
	t.Parallel()

	var temporalPromoted bool
	var services *coredata.Services
	start := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.SourceVersion = "source-new"
		promoteOnActivate := false
		cfg.PromoteSharedStateOnActivate = &promoteOnActivate
		cfg.FinishSharedStartupPromotion = func(context.Context) error {
			temporalPromoted = true
			return nil
		}
		cfg.TemporalWorkersPromoted = func() bool { return temporalPromoted }
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
	})
	testutil.CloseOnCleanup(t, srv)

	resp, err := http.Post(srv.URL+"/promote/registry?source_version=source-new&minimum_healthy_instances=5", "", nil)
	if err != nil {
		t.Fatalf("POST /promote/registry: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("registry before temporal status = %d, want %d", resp.StatusCode, http.StatusPreconditionFailed)
	}

	resp, err = http.Post(srv.URL+"/promote/temporal?source_version=source-new", "", nil)
	if err != nil {
		t.Fatalf("POST /promote/temporal: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("temporal promote status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp, err = http.Post(srv.URL+"/promote/registry?source_version=source-new&minimum_healthy_instances=5", "", nil)
	if err != nil {
		t.Fatalf("POST /promote/registry after temporal: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registry after temporal status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	current, err := services.GestaltdSourceVersionState.CurrentForAdmission(context.Background())
	if err != nil {
		t.Fatalf("CurrentForAdmission: %v", err)
	}
	if current != "source-new" {
		t.Fatalf("current source version = %q, want source-new", current)
	}
}

func TestPromoteTemporalRejectsMismatchedSourceVersionBeforePromotion(t *testing.T) {
	t.Parallel()

	var temporalPromoted bool
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.SourceVersion = "source-new"
		promoteOnActivate := false
		cfg.PromoteSharedStateOnActivate = &promoteOnActivate
		cfg.FinishSharedStartupPromotion = func(context.Context) error {
			temporalPromoted = true
			return nil
		}
	})
	testutil.CloseOnCleanup(t, srv)

	resp, err := http.Post(srv.URL+"/promote/temporal?source_version=source-wrong", "", nil)
	if err != nil {
		t.Fatalf("POST /promote/temporal: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("temporal promote status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	if temporalPromoted {
		t.Fatal("temporal promotion ran for mismatched source version")
	}
}

func TestPromoteRegistryUsesFleetTemporalPromotionEvidence(t *testing.T) {
	t.Parallel()

	var temporalPromoted bool
	var services *coredata.Services
	start := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.SourceVersion = "source-new"
		promoteOnActivate := false
		cfg.PromoteSharedStateOnActivate = &promoteOnActivate
		cfg.FinishSharedStartupPromotion = func(context.Context) error {
			temporalPromoted = true
			return nil
		}
		cfg.TemporalWorkersPromoted = func() bool { return temporalPromoted }
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
	})
	testutil.CloseOnCleanup(t, srv)

	resp, err := http.Post(srv.URL+"/promote/temporal?source_version=source-new", "", nil)
	if err != nil {
		t.Fatalf("POST /promote/temporal: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("temporal promote status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	temporalPromoted = false

	resp, err = http.Post(srv.URL+"/promote/registry?source_version=source-new&minimum_healthy_instances=5", "", nil)
	if err != nil {
		t.Fatalf("POST /promote/registry after fleet temporal evidence: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registry after fleet temporal evidence status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	current, err := services.GestaltdSourceVersionState.CurrentForAdmission(context.Background())
	if err != nil {
		t.Fatalf("CurrentForAdmission: %v", err)
	}
	if current != "source-new" {
		t.Fatalf("current source version = %q, want source-new", current)
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
