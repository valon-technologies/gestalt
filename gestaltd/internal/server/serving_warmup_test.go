package server

import (
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestServingWarmupPathsIncludesMountedApps(t *testing.T) {
	t.Parallel()

	paths := servingWarmupPaths(
		map[string]*config.ProviderEntry{
			"vds-forge": {Static: &config.AppStaticConfig{Mount: "/vds-forge"}},
		},
		[]MountedUI{{AppName: "vds-forge", Path: "/vds-forge"}},
	)
	want := []string{"/", "/icon.svg", "/vds-forge/", "/api/v1/apps/vds-forge/operations"}
	if len(paths) != len(want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	for i, path := range want {
		if paths[i] != path {
			t.Fatalf("paths[%d] = %q, want %q", i, paths[i], path)
		}
	}
}

func TestProbeServingRoutesTreatsNon5xxAsReady(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.WriteHeader(http.StatusOK)
		case "/vds-forge/":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	statuses := probeServingRoutes(handler, []string{"/", "/vds-forge/", "/broken"})
	failures := servingRouteFailures(statuses)
	if len(failures) != 1 || failures[0] != "/broken returned HTTP 503" {
		t.Fatalf("failures = %#v", failures)
	}
}
