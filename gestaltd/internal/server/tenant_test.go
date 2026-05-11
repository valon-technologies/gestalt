package server_test

import (
	"context"
	"net/http"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
)

func TestTenantScopeAttachedBeforeAuthentication(t *testing.T) {
	t.Parallel()

	scopeCh := make(chan gestalt.TenantScope, 1)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Tenants = map[string]*config.TenantConfig{
			"acme": {Hosts: []string{"acme.dev.valon.tools"}},
		}
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "tenant-auth",
			ValidateTokenFn: func(ctx context.Context, _ string) (*core.UserIdentity, error) {
				scope, _ := gestalt.TenantScopeFromContext(ctx)
				scopeCh <- scope
				return &core.UserIdentity{Email: "ada@example.test"}, nil
			},
		}
	})
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = "acme.dev.valon.tools"
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	scope := <-scopeCh
	if scope.TenantID != "acme" {
		t.Fatalf("TenantID = %q, want acme", scope.TenantID)
	}
	if scope.Host != "acme.dev.valon.tools" {
		t.Fatalf("Host = %q, want acme.dev.valon.tools", scope.Host)
	}
	if !scope.TenantBound {
		t.Fatal("TenantBound = false, want true")
	}
}

func TestTenantMiddlewareRejectsUnknownTenantHost(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Tenants = map[string]*config.TenantConfig{
			"acme": {Hosts: []string{"acme.dev.valon.tools"}},
		}
	})
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/info", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = "unknown.dev.valon.tools"
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/auth/info: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTenantMiddlewareSkipsHealth(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Tenants = map[string]*config.TenantConfig{
			"acme": {Hosts: []string{"acme.dev.valon.tools"}},
		}
	})
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = "unknown.dev.valon.tools"
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestTenantMiddlewareSkipsAdminRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		profile server.RouteProfile
		path    string
	}{
		{name: "combined admin", path: "/admin/foo"},
		{name: "management admin", profile: server.RouteProfileManagement, path: "/admin/foo"},
		{name: "management root redirect", profile: server.RouteProfileManagement, path: "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Tenants = map[string]*config.TenantConfig{
					"acme": {Hosts: []string{"acme.dev.valon.tools"}},
				}
				cfg.RouteProfile = tc.profile
				cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})
			})
			t.Cleanup(ts.Close)

			req, err := http.NewRequest(http.MethodGet, ts.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Host = "localhost:9091"
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == http.StatusNotFound {
				t.Fatalf("status = 404, tenant middleware should not gate %s", tc.path)
			}
		})
	}
}
