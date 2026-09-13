package server_test

import (
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestDeploymentHostRequiresBearerForEveryPath(t *testing.T) {
	t.Setenv("GESTALTD_DEPLOYMENT_HOSTS", "deploy.vt.valon.tools;retained.deploy.vt.valon.tools")
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.UIReadiness = server.NewUIReadinessMonitor(server.UIReadinessMonitorConfig{ProbeBearer: "qualification-token"})
	})
	testutil.CloseOnCleanup(t, srv)
	for _, path := range []string{"/startup-gate", "/fleet-readiness", "/", "/api/v1/test/webhooks", "/scim/v2", "/promote/registry"} {
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = "deploy.vt.valon.tools"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: got %d", path, resp.StatusCode)
		}
	}
	for _, host := range []string{"vt.valon.tools", "valon.tools", "deploy.vt.valon.tools", "retained.deploy.vt.valon.tools"} {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/startup-gate", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		if host == "deploy.vt.valon.tools" || host == "retained.deploy.vt.valon.tools" {
			req.Header.Set("Authorization", "Bearer qualification-token")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: got %d", host, resp.StatusCode)
		}
	}
}

func TestDeploymentManagementRequiresBearerOnPublicHosts(t *testing.T) {
	t.Setenv("GESTALTD_DEPLOYMENT_HOSTS", "deploy.vt.valon.tools;retained.deploy.vt.valon.tools")
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.UIReadiness = server.NewUIReadinessMonitor(server.UIReadinessMonitorConfig{ProbeBearer: "qualification-token"})
	})
	testutil.CloseOnCleanup(t, srv)
	for _, host := range []string{"vt.valon.tools", "valon.tools"} {
		for _, path := range []string{"/activate", "/promote", "/promote/temporal", "/promote/registry"} {
			for _, token := range []string{"", "wrong", "qualification-token"} {
				// GET cannot perform a promotion. An authenticated request must
				// reach method routing, while unauthorized POSTs never reach it.
				method := http.MethodPost
				expected := http.StatusUnauthorized
				if token == "qualification-token" {
					method = http.MethodGet
					expected = http.StatusMethodNotAllowed
				}
				req, err := http.NewRequest(method, srv.URL+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Host = host
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				if err := resp.Body.Close(); err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != expected {
					t.Fatalf("%s %s token=%q: got %d, want %d", host, path, token, resp.StatusCode, expected)
				}
			}
		}
	}
}
