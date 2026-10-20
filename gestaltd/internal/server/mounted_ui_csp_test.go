package server_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
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

func TestMountedUIContentSecurityPolicyConnectSrc(t *testing.T) {
	t.Parallel()

	const intake = "https://intake.example.com"
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MountedUIs = []server.MountedUI{
			{Path: "/rum-app", Handler: okHandler, ConnectSrc: []string{intake}},
			{Path: "/dev-rum-app", Handler: okHandler, ConnectSrc: []string{intake}, IsDev: true},
			{Path: "/plain-app", Handler: okHandler},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	for _, tc := range []struct {
		path        string
		wantConnect string
		wantScript  string
	}{
		{path: "/rum-app/", wantConnect: "connect-src 'self' " + intake + ";", wantScript: "script-src 'self' 'unsafe-inline';"},
		{path: "/dev-rum-app/", wantConnect: "connect-src 'self' ws: wss: " + intake + ";", wantScript: "script-src 'self' 'unsafe-inline' 'unsafe-eval';"},
		// Other mounts and server routes keep the default policy.
		{path: "/plain-app/", wantConnect: "connect-src 'self';", wantScript: "script-src 'self' 'unsafe-inline';"},
		{path: "/health", wantConnect: "connect-src 'self';", wantScript: "script-src 'self' 'unsafe-inline';"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer func() { _ = resp.Body.Close() }()

			csp := resp.Header.Get("Content-Security-Policy")
			for _, want := range []string{tc.wantConnect, tc.wantScript, "object-src 'none'", "frame-ancestors 'self'"} {
				if !strings.Contains(csp, want) {
					t.Errorf("Content-Security-Policy missing %q; got %q", want, csp)
				}
			}
		})
	}
}

func TestAppStaticMountContentSecurityPolicyFromConfig(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(rootDir, "index.html"), "<html>rum</html>")
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "rumApp", DN: "RUM", ConnMode: core.ConnectionModeNone})
		cfg.AppDefs = map[string]*config.ProviderEntry{"rumApp": {
			Static: &config.AppStaticConfig{
				Mount:  "/rum",
				Public: true,
				ContentSecurityPolicy: &config.AppStaticContentSecurityPolicy{
					ConnectSrc: []string{"https://intake.example.com"},
				},
			},
			ResolvedStaticRoot: rootDir,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/rum/")
	if err != nil {
		t.Fatalf("GET /rum/: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /rum/ status = %d, want 200", resp.StatusCode)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'self' https://intake.example.com;") {
		t.Errorf("Content-Security-Policy connect-src not extended; got %q", csp)
	}
}
