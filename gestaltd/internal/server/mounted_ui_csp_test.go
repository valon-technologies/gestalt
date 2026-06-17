package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestMountedUIContentSecurityPolicyDevActive(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MountedUIs = []server.MountedUI{{
			Path: "/dev-app",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			IsDev: true,
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
}
