package bootstrap_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestBootstrap_ConfigHeadersOverrideManifestHeaders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		headerName    = "X-Static-Version"
		manifestValue = "from-manifest"
		configValue   = "from-config"
	)

	gotHeader := make(chan string, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader <- r.Header.Get(headerName)
		writeTestJSON(w, map[string]any{"ok": true})
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/sample",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL: apiSrv.URL,
			Headers: map[string]string{
				"x-static-version": manifestValue,
			},
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:   "list_items",
					Method: http.MethodGet,
					Path:   "/items",
				},
			},
		},
	}
	manifestData, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "plugin.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"sample": {
			Plugin: &config.PluginDef{
				IsDeclarative:        true,
				ResolvedManifestPath: manifestPath,
				Headers: map[string]string{
					headerName: configValue,
				},
			},
		},
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	prov, err := result.Providers.Get("sample")
	if err != nil {
		t.Fatalf("Providers.Get: %v", err)
	}

	execResult, err := prov.Execute(ctx, "list_items", nil, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if execResult.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", execResult.Status, http.StatusOK)
	}

	select {
	case got := <-gotHeader:
		if got != configValue {
			t.Fatalf("%s = %q, want %q", headerName, got, configValue)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}
}
