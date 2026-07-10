package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type rewriteHostTransport struct {
	host   string
	scheme string
	inner  http.RoundTripper
}

func (t *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.scheme
	clone.URL.Host = t.host
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(clone)
}

func TestAdminAppRegistryEndpointsListConfiguredRegistriesAndVersions(t *testing.T) {
	t.Parallel()

	indexJSON := `{
  "schemaVersion": 1,
  "apps": {
    "g-issues": {
      "displayName": "g-issues",
      "versions": {
        "0.0.0-snapshot.gabc123": {
          "metadata": "apps/g-issues/versions/0.0.0-snapshot.gabc123.json",
          "platforms": ["linux/amd64", "darwin/arm64"],
          "publishedAt": "2026-07-10T02:21:54Z"
        },
        "0.0.0-snapshot.gdef456": {
          "metadata": "apps/g-issues/versions/0.0.0-snapshot.gdef456.json",
          "platforms": ["linux/amd64"],
          "publishedAt": "2026-07-09T12:00:00Z"
        }
      }
    }
  }
}`
	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gitlab-peach-street-gestalt-app-registry/apps/g-issues/index.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(indexJSON))
	}))
	defer registrySrv.Close()

	registryHost := strings.TrimPrefix(registrySrv.URL, "http://")
	reader := &appregistry.RegistryReader{
		HTTPClient: &http.Client{
			Transport: &rewriteHostTransport{host: registryHost, scheme: "http"},
		},
	}
	registry, err := config.NewGCSAppRegistry("gitlab-peach-street-gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.AppRegistries = map[string]config.AppRegistryConfig{
			"toolshed": registry,
		}
		cfg.AppRegistryReader = reader
	})
	testutil.CloseOnCleanup(t, ts)

	listResp, err := http.Get(ts.URL + "/admin/api/v1/app-registries")
	if err != nil {
		t.Fatalf("GET app registries: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		t.Fatalf("app registries status = %d: %s", listResp.StatusCode, body)
	}
	var registries []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&registries); err != nil {
		t.Fatalf("decode app registries: %v", err)
	}
	if len(registries) != 1 || registries[0]["name"] != "toolshed" {
		t.Fatalf("registries = %#v", registries)
	}

	versionsResp, err := http.Get(ts.URL + "/admin/api/v1/app-registries/toolshed/apps/g-issues/versions")
	if err != nil {
		t.Fatalf("GET app registry versions: %v", err)
	}
	defer func() { _ = versionsResp.Body.Close() }()
	if versionsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(versionsResp.Body)
		t.Fatalf("versions status = %d: %s", versionsResp.StatusCode, body)
	}
	var payload struct {
		Registry string `json:"registry"`
		App      string `json:"app"`
		Versions []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(versionsResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode versions: %v", err)
	}
	if payload.Registry != "toolshed" || payload.App != "g-issues" {
		t.Fatalf("payload = %#v", payload)
	}
	if len(payload.Versions) != 2 {
		t.Fatalf("versions len = %d, want 2", len(payload.Versions))
	}
	if payload.Versions[0].Version != "0.0.0-snapshot.gabc123" {
		t.Fatalf("versions[0] = %#v, want newest first", payload.Versions[0])
	}
}

func TestAdminAppRegistryVersionsReturnsEmptyForUnknownApp(t *testing.T) {
	t.Parallel()

	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gitlab-peach-street-gestalt-app-registry/apps/missing/index.json" {
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer registrySrv.Close()

	registryHost := strings.TrimPrefix(registrySrv.URL, "http://")
	reader := &appregistry.RegistryReader{
		HTTPClient: &http.Client{
			Transport: &rewriteHostTransport{host: registryHost, scheme: "http"},
		},
	}
	registry, err := config.NewGCSAppRegistry("gitlab-peach-street-gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.AppRegistries = map[string]config.AppRegistryConfig{
			"toolshed": registry,
		}
		cfg.AppRegistryReader = reader
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/app-registries/toolshed/apps/missing/versions")
	if err != nil {
		t.Fatalf("GET versions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var payload struct {
		Versions []any `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Versions) != 0 {
		t.Fatalf("versions = %#v, want empty", payload.Versions)
	}
}
