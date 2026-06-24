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

func TestMountedUISameOriginFraming(t *testing.T) {
	t.Parallel()

	okHandler := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MountedUIs = []server.MountedUI{
			{
				Path:                   "/framed",
				Handler:                http.HandlerFunc(okHandler),
				AllowSameOriginFraming: true,
			},
			{
				Path:    "/locked",
				Handler: http.HandlerFunc(okHandler),
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	get := func(t *testing.T, path string) http.Header {
		t.Helper()
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.Header
	}

	t.Run("opted-in mount permits same-origin framing", func(t *testing.T) {
		h := get(t, "/framed/")
		if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'self'") {
			t.Errorf("Content-Security-Policy = %q; want frame-ancestors 'self'", csp)
		}
		if xfo := h.Get("X-Frame-Options"); xfo != "SAMEORIGIN" {
			t.Errorf("X-Frame-Options = %q; want SAMEORIGIN", xfo)
		}
	})

	t.Run("default mount forbids all framing", func(t *testing.T) {
		h := get(t, "/locked/")
		if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("Content-Security-Policy = %q; want frame-ancestors 'none'", csp)
		}
		if xfo := h.Get("X-Frame-Options"); xfo != "DENY" {
			t.Errorf("X-Frame-Options = %q; want DENY", xfo)
		}
	})
}
