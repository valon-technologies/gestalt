package server_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestRegistryOnlyStaticSurfaceStartsUnavailable(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.ArtifactsDir = t.TempDir()
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {
				Source: config.ProviderSource{Registry: "toolshed"},
				Static: &config.AppStaticConfig{Mount: "/g-issues", Public: true},
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"status": {
						Path:     "/status",
						Method:   http.MethodGet,
						Security: "none",
						Target:   "status",
					},
				},
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/g-issues")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 503: %s", resp.StatusCode, body)
	}

	httpResp, err := http.Get(ts.URL + "/api/v1/g-issues/status")
	if err != nil {
		t.Fatalf("GET HTTP binding: %v", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("HTTP binding status = %d, want 503", httpResp.StatusCode)
	}
}

func TestRegistryOnlyAddAndUpgradeRoutes(t *testing.T) {
	t.Parallel()
	fixture := registrytest.NewInstallFixture(t)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": fixture.Registry}
		cfg.AppRegistryReader = fixture.Reader
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	upgrade, err := http.Post(
		ts.URL+"/admin/api/v1/app-registries/toolshed/apps/g-issues/upgrade",
		"application/json",
		bytes.NewBufferString(`{"version":"`+fixture.Version+`"}`),
	)
	if err != nil {
		t.Fatalf("POST upgrade: %v", err)
	}
	_ = upgrade.Body.Close()
	if upgrade.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty-catalog upgrade status = %d, want 400", upgrade.StatusCode)
	}

	add, err := http.Post(
		ts.URL+"/admin/api/v1/app-registries/toolshed/apps/g-issues/add",
		"application/json",
		bytes.NewBufferString(`{"version":"`+fixture.Version+`"}`),
	)
	if err != nil {
		t.Fatalf("POST add: %v", err)
	}
	_ = add.Body.Close()
	if add.StatusCode != http.StatusOK {
		t.Fatalf("add status = %d, want 200", add.StatusCode)
	}
}
