package server_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestActivateAppProvidersEndpointTriggersActivation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.ActivateAppProviders = func(context.Context) { calls.Add(1) }
	})
	testutil.CloseOnCleanup(t, srv)

	for i := 1; i <= 2; i++ {
		resp, err := http.Post(srv.URL+"/activate", "", nil)
		if err != nil {
			t.Fatalf("POST /activate: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("call %d: status = %d, want %d", i, resp.StatusCode, http.StatusOK)
		}
		if got := calls.Load(); got != int32(i) {
			t.Fatalf("call %d: activation calls = %d, want %d", i, got, i)
		}
	}
}

func TestActivateAppProvidersEndpointNotExposedOnPublicRouteProfile(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.ActivateAppProviders = func(context.Context) { calls.Add(1) }
	})
	testutil.CloseOnCleanup(t, srv)

	resp, err := http.Post(srv.URL+"/activate", "", nil)
	if err != nil {
		t.Fatalf("POST /activate: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("activation calls = %d, want 0 on public route profile", got)
	}
}

func TestActivateAppProvidersEndpointExposedOnManagementRouteProfile(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfileManagement
		cfg.ActivateAppProviders = func(context.Context) { calls.Add(1) }
	})
	testutil.CloseOnCleanup(t, srv)

	resp, err := http.Post(srv.URL+"/activate", "", nil)
	if err != nil {
		t.Fatalf("POST /activate: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("activation calls = %d, want 1", got)
	}
}

func TestActivateEndpointPromotesSourceVersionAndRetargetsRollout(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var services *coredata.Services
	start := time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC)
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.SourceVersion = "source-new"
		cfg.Now = func() time.Time { return start.Add(5 * time.Minute) }
		cfg.ActivateAppProviders = func(context.Context) { calls.Add(1) }
		services = cfg.Services
		ctx := context.Background()
		if _, err := services.GestaltdSourceVersionState.Activate(
			ctx,
			"source-old",
			start,
			false,
			appregistry.DefaultRolloutEnrollmentWindow,
			appregistry.DefaultRolloutTimeout,
		); err != nil {
			t.Fatalf("seed source version: %v", err)
		}
		if _, err := services.AppRollouts.Create(ctx, &core.AppRollout{
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

	resp, err := http.Post(srv.URL+"/activate?source_version=source-new", "", nil)
	if err != nil {
		t.Fatalf("POST /activate: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activation status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	rollout, err := services.AppRollouts.Get(context.Background(), "g-issues")
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if rollout.TargetSourceVersion != "source-new" || rollout.State != core.AppRolloutStateEnrolling {
		t.Fatalf("retargeted rollout = %#v", rollout)
	}
	current, err := services.GestaltdSourceVersionState.CurrentForAdmission(context.Background())
	if err != nil {
		t.Fatalf("CurrentForAdmission: %v", err)
	}
	if current != "source-new" {
		t.Fatalf("current source version = %q, want source-new", current)
	}
	if calls.Load() != 1 {
		t.Fatalf("activation calls = %d, want 1", calls.Load())
	}
}

func TestActivateEndpointRequiresMatchingSourceVersion(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.SourceVersion = "source-new"
	})
	testutil.CloseOnCleanup(t, srv)

	for _, tc := range []struct {
		path       string
		wantStatus int
	}{
		{path: "/activate", wantStatus: http.StatusBadRequest},
		{path: "/activate?source_version=source-old", wantStatus: http.StatusConflict},
	} {
		resp, err := http.Post(srv.URL+tc.path, "", nil)
		if err != nil {
			t.Fatalf("POST %s: %v", tc.path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.wantStatus {
			t.Fatalf("POST %s status = %d, want %d", tc.path, resp.StatusCode, tc.wantStatus)
		}
	}
}

func TestActivateEndpointRejectsInvalidRetry(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	testutil.CloseOnCleanup(t, srv)

	resp, err := http.Post(srv.URL+"/activate?retry=false", "", nil)
	if err != nil {
		t.Fatalf("POST /activate: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
