package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func appRegistryTestAppDefs(version string) map[string]*config.ProviderEntry {
	entry := &config.ProviderEntry{}
	entry.Source.SetResolvedPackage("", version)
	return map[string]*config.ProviderEntry{
		"g-issues": entry,
	}
}

func TestAdminAppRegistryInstall(t *testing.T) {
	t.Parallel()

	t.Run("installs_and_lists_known_version", func(t *testing.T) {
		t.Parallel()

		fixture := registrytest.NewInstallFixture(t)
		artifactsDir := t.TempDir()
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.AppRegistries = map[string]config.AppRegistryConfig{
				"toolshed": fixture.Registry,
			}
			cfg.AppRegistryReader = fixture.Reader
			cfg.ArtifactsDir = artifactsDir
			cfg.AppDefs = appRegistryTestAppDefs("0.0.0-config")
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
			Registry     string `json:"registry"`
			App          string `json:"app"`
			Installation struct {
				Version string `json:"version"`
			} `json:"installation"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode install response: %v", err)
		}
		if payload.Registry != "toolshed" || payload.App != "g-issues" {
			t.Fatalf("payload = %#v", payload)
		}
		if payload.Installation.Version != fixture.Version {
			t.Fatalf("installation.version = %q", payload.Installation.Version)
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
			cfg.AppDefs = appRegistryTestAppDefs("0.0.0-config")
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

	t.Run("already_installed_returns_bad_request", func(t *testing.T) {
		t.Parallel()

		fixture := registrytest.NewInstallFixture(t)
		artifactsDir := t.TempDir()
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.AppRegistries = map[string]config.AppRegistryConfig{
				"toolshed": fixture.Registry,
			}
			cfg.AppRegistryReader = fixture.Reader
			cfg.ArtifactsDir = artifactsDir
			cfg.AppDefs = appRegistryTestAppDefs("0.0.0-config")
		})
		testutil.CloseOnCleanup(t, ts)

		body := []byte(`{"version":"` + fixture.Version + `"}`)
		first, err := http.Post(ts.URL+"/admin/api/v1/app-registries/toolshed/apps/g-issues/install", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST first install: %v", err)
		}
		_ = first.Body.Close()
		if first.StatusCode != http.StatusOK {
			t.Fatalf("first install status = %d", first.StatusCode)
		}

		second, err := http.Post(ts.URL+"/admin/api/v1/app-registries/toolshed/apps/g-issues/install", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST second install: %v", err)
		}
		defer func() { _ = second.Body.Close() }()
		if second.StatusCode != http.StatusBadRequest {
			raw, _ := io.ReadAll(second.Body)
			t.Fatalf("second install status = %d, want 400: %s", second.StatusCode, raw)
		}
	})

	t.Run("validation_failure", func(t *testing.T) {
		t.Parallel()

		fixture := registrytest.NewInstallFixture(t)
		artifactsDir := t.TempDir()

		publicURL, err := fixture.Registry.PublicURL()
		if err != nil {
			t.Fatalf("PublicURL: %v", err)
		}
		entry, err := fixture.Reader.FetchEntry(context.Background(), publicURL, "g-issues", fixture.Version)
		if err != nil {
			t.Fatalf("FetchEntry: %v", err)
		}
		entry.Compatibility = appregistry.Compatibility{MinGestaltdVersion: "99.0.0"}
		entryJSON, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}

		registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/" + registrytest.Bucket + "/apps/g-issues/versions/" + fixture.Version + ".json":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(entryJSON)
			case "/" + registrytest.Bucket + "/apps/g-issues/artifacts/" + fixture.Version + "/artifact.tar.gz":
				w.Header().Set("Content-Type", "application/gzip")
				_, _ = w.Write(fixture.ArchiveBytes)
			default:
				http.NotFound(w, r)
			}
		}))
		defer registrySrv.Close()
		reader := registrytest.NewReaderForServer(t, registrySrv.URL)

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.AppRegistries = map[string]config.AppRegistryConfig{
				"toolshed": fixture.Registry,
			}
			cfg.AppRegistryReader = reader
			cfg.ArtifactsDir = artifactsDir
			cfg.GestaltdVersion = "0.1.0"
			cfg.AppDefs = map[string]*config.ProviderEntry{
				"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
			}
		})
		testutil.CloseOnCleanup(t, ts)

		body := []byte(`{"version":"` + fixture.Version + `"}`)
		resp, err := http.Post(ts.URL+"/admin/api/v1/app-registries/toolshed/apps/g-issues/add", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST add: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("add status = %d, want 400: %s", resp.StatusCode, raw)
		}
	})

	t.Run("get_versions_by_app", func(t *testing.T) {
		t.Parallel()

		fixture := registrytest.NewInstallFixture(t)
		artifactsDir := t.TempDir()
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.AppRegistries = map[string]config.AppRegistryConfig{
				"toolshed": fixture.Registry,
			}
			cfg.AppRegistryReader = fixture.Reader
			cfg.ArtifactsDir = artifactsDir
			cfg.AppDefs = appRegistryTestAppDefs("0.0.0-config")
		})
		testutil.CloseOnCleanup(t, ts)

		installBody := []byte(`{"version":"` + fixture.Version + `"}`)
		installResp, err := http.Post(ts.URL+"/admin/api/v1/app-registries/toolshed/apps/g-issues/install", "application/json", bytes.NewReader(installBody))
		if err != nil {
			t.Fatalf("POST install: %v", err)
		}
		_ = installResp.Body.Close()
		if installResp.StatusCode != http.StatusOK {
			t.Fatalf("install status = %d", installResp.StatusCode)
		}

		getResp, err := http.Get(ts.URL + "/admin/api/v1/app-installations/g-issues")
		if err != nil {
			t.Fatalf("GET installation: %v", err)
		}
		defer func() { _ = getResp.Body.Close() }()
		if getResp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(getResp.Body)
			t.Fatalf("get status = %d: %s", getResp.StatusCode, raw)
		}
		var installations []map[string]any
		if err := json.NewDecoder(getResp.Body).Decode(&installations); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(installations) != 1 {
			t.Fatalf("installations = %#v", installations)
		}
		if installations[0]["app"] != "g-issues" {
			t.Fatalf("installation = %#v", installations[0])
		}
	})
}
