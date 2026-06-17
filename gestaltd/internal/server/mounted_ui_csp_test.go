package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestMountedUIContentSecurityPolicy(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("dev_active", func(t *testing.T) {
		t.Parallel()

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.MountedUIs = []server.MountedUI{{
				Path:    "/dev-app",
				Handler: inner,
				IsDev:   true,
			}}
		})
		testutil.CloseOnCleanup(t, ts)

		resp, err := http.Get(ts.URL + "/dev-app/")
		if err != nil {
			t.Fatalf("GET /dev-app/: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		csp := resp.Header.Get("Content-Security-Policy")
		for _, want := range []string{
			"connect-src 'self' ws: wss:",
			"script-src 'self' 'unsafe-inline' 'unsafe-eval'",
		} {
			if !strings.Contains(csp, want) {
				t.Errorf("Content-Security-Policy missing %q; got %q", want, csp)
			}
		}
	})

	t.Run("static", func(t *testing.T) {
		t.Parallel()

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.MountedUIs = []server.MountedUI{{
				Path:    "/static-app",
				Handler: inner,
				IsDev:   false,
			}}
		})
		testutil.CloseOnCleanup(t, ts)

		resp, err := http.Get(ts.URL + "/static-app/")
		if err != nil {
			t.Fatalf("GET /static-app/: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "connect-src 'self'") {
			t.Errorf("Content-Security-Policy missing connect-src 'self'; got %q", csp)
		}
		if strings.Contains(csp, "ws:") || strings.Contains(csp, "wss:") {
			t.Errorf("Content-Security-Policy must not allow websockets on static mounts; got %q", csp)
		}
		if strings.Contains(csp, "'unsafe-eval'") {
			t.Errorf("Content-Security-Policy must not include 'unsafe-eval' on static mounts; got %q", csp)
		}
	})
}
