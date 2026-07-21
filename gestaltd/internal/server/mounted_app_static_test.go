package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

type appVersionReporterStub map[string]string

func (s appVersionReporterStub) RunningVersion(app string) string {
	return s[app]
}

func TestMountedAppStaticsAllowsRegistryAppBeforeStartup(t *testing.T) {
	t.Parallel()

	mounted, err := mountedAppStaticsFromEntries(
		map[string]*config.ProviderEntry{
			"g-issues": {
				Source: config.ProviderSource{Registry: "toolshed"},
				Static: &config.AppStaticConfig{Mount: "/g-issues"},
			},
		},
		nil,
		t.TempDir(),
		appVersionReporterStub{},
	)
	if err != nil {
		t.Fatalf("mountedAppStaticsFromEntries: %v", err)
	}
	if len(mounted) != 1 {
		t.Fatalf("mounted UIs = %d, want 1", len(mounted))
	}

	response := httptest.NewRecorder()
	mounted[0].Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/g-issues", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}
