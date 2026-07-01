package server_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

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
