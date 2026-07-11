package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAdminAppRegistryInstall(t *testing.T) {
	t.Parallel()

	t.Run("promotes_and_lists_installation", func(t *testing.T) {
		t.Parallel()

		fixture := registrytest.NewInstallFixture(t)
		artifactsDir := t.TempDir()
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.AppRegistries = map[string]config.AppRegistryConfig{
				"toolshed": fixture.Registry,
			}
			cfg.AppRegistryReader = fixture.Reader
			cfg.ArtifactsDir = artifactsDir
		})
		testutil.CloseOnCleanup(t, ts)

		body := []byte(`{"version":"` + fixture.Version + `","actor":"user:alice"}`)
		resp, err := http.Post(ts.URL+"/admin/api/v1/app-registries/toolshed/apps/g-issues/install", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST install: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("install status = %d: %s", resp.StatusCode, raw)
		}

		var payload struct {
			Registry         string `json:"registry"`
			App              string `json:"app"`
			MaterializedPath string `json:"materializedPath"`
			Installation     struct {
				RolloutStatus   string `json:"rolloutStatus"`
				ResolvedVersion string `json:"resolvedVersion"`
			} `json:"installation"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode install response: %v", err)
		}
		if payload.Registry != "toolshed" || payload.App != "g-issues" {
			t.Fatalf("payload = %#v", payload)
		}
		if payload.Installation.RolloutStatus != core.AppInstallationRolloutStatusPromoted {
			t.Fatalf("installation.rolloutStatus = %q", payload.Installation.RolloutStatus)
		}
		if payload.Installation.ResolvedVersion != fixture.Version {
			t.Fatalf("installation.resolvedVersion = %q", payload.Installation.ResolvedVersion)
		}
		if payload.MaterializedPath == "" {
			t.Fatal("materializedPath is empty")
		}

		listResp, err := http.Get(ts.URL + "/admin/api/v1/app-installations")
		if err != nil {
			t.Fatalf("GET app-installations: %v", err)
		}
		defer func() { _ = listResp.Body.Close() }()
		if listResp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(listResp.Body)
			t.Fatalf("list status = %d: %s", listResp.StatusCode, raw)
		}
		var installations []map[string]any
		if err := json.NewDecoder(listResp.Body).Decode(&installations); err != nil {
			t.Fatalf("decode installations: %v", err)
		}
		if len(installations) != 1 {
			t.Fatalf("installations = %#v", installations)
		}
	})

	t.Run("missing_version_returns_not_found", func(t *testing.T) {
		t.Parallel()

		registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer registrySrv.Close()

		reader := registrytest.NewReaderForServer(t, registrySrv.URL)
		registry, err := config.NewGCSAppRegistry(registrytest.Bucket)
		if err != nil {
			t.Fatalf("NewGCSAppRegistry: %v", err)
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.AppRegistries = map[string]config.AppRegistryConfig{
				"toolshed": registry,
			}
			cfg.AppRegistryReader = reader
			cfg.ArtifactsDir = t.TempDir()
		})
		testutil.CloseOnCleanup(t, ts)

		body := []byte(`{"version":"missing-version"}`)
		resp, err := http.Post(ts.URL+"/admin/api/v1/app-registries/toolshed/apps/g-issues/install", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST install: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("install status = %d, want 404: %s", resp.StatusCode, raw)
		}
	})

	t.Run("invalid_rollout_status_returns_bad_request", func(t *testing.T) {
		t.Parallel()

		registry, err := config.NewGCSAppRegistry(registrytest.Bucket)
		if err != nil {
			t.Fatalf("NewGCSAppRegistry: %v", err)
		}
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.AppRegistries = map[string]config.AppRegistryConfig{
				"toolshed": registry,
			}
			cfg.ArtifactsDir = t.TempDir()
		})
		testutil.CloseOnCleanup(t, ts)

		resp, err := http.Get(ts.URL + "/admin/api/v1/app-installations?rolloutStatus=invalid")
		if err != nil {
			t.Fatalf("GET app-installations: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("list status = %d, want 400: %s", resp.StatusCode, raw)
		}
	})

	t.Run("list_works_without_app_registries", func(t *testing.T) {
		t.Parallel()

		ts := newTestServer(t)
		testutil.CloseOnCleanup(t, ts)

		listResp, err := http.Get(ts.URL + "/admin/api/v1/app-installations")
		if err != nil {
			t.Fatalf("GET app-installations: %v", err)
		}
		defer func() { _ = listResp.Body.Close() }()
		if listResp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(listResp.Body)
			t.Fatalf("list status = %d, want 200: %s", listResp.StatusCode, raw)
		}
	})

	t.Run("install_without_app_registries_returns_not_found", func(t *testing.T) {
		t.Parallel()

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.ArtifactsDir = t.TempDir()
		})
		testutil.CloseOnCleanup(t, ts)

		body := []byte(`{"version":"0.0.0-snapshot.v1"}`)
		resp, err := http.Post(ts.URL+"/admin/api/v1/app-registries/toolshed/apps/g-issues/install", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST install: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("install status = %d, want 404: %s", resp.StatusCode, raw)
		}
	})
}
