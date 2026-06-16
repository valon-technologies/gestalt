package operator

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
)

func TestResolveArchiveSourceLocationPreservesMetadataQueryForRelativeRefs(t *testing.T) {
	t.Parallel()

	got, err := resolveArchiveSourceLocation(
		"https://storage.googleapis.com/snapshots/github.com/acme/providers/ref/app/demo/provider-release.yaml?sourceRef=ref",
		"app-linux-amd64.tar.gz",
		nil,
	)
	if err != nil {
		t.Fatalf("resolveArchiveSourceLocation: %v", err)
	}
	want := "https://storage.googleapis.com/snapshots/github.com/acme/providers/ref/app/demo/app-linux-amd64.tar.gz?sourceRef=ref"
	if got != want {
		t.Fatalf("resolveArchiveSourceLocation = %q, want %q", got, want)
	}
}

func TestResolveArchiveSourceLocationDoesNotOverrideExplicitQuery(t *testing.T) {
	t.Parallel()

	got, err := resolveArchiveSourceLocation(
		"https://storage.googleapis.com/snapshots/github.com/acme/providers/ref/app/demo/provider-release.yaml?sourceRef=ref",
		"app-linux-amd64.tar.gz?generation=1",
		nil,
	)
	if err != nil {
		t.Fatalf("resolveArchiveSourceLocation: %v", err)
	}
	want := "https://storage.googleapis.com/snapshots/github.com/acme/providers/ref/app/demo/app-linux-amd64.tar.gz?generation=1"
	if got != want {
		t.Fatalf("resolveArchiveSourceLocation = %q, want %q", got, want)
	}
}

func TestFetchProviderReleaseBundleLoadsSidecarMetadata(t *testing.T) {
	t.Parallel()

	manifestData := []byte(`kind: app
source: github.com/acme/providers/app
version: 1.0.0
spec: {}
`)
	catalogData := []byte(`name: app
operations:
  - id: echo
    method: POST
`)
	metadataData := []byte(fmt.Sprintf(`package: github.com/acme/providers/app
kind: app
version: 1.0.0
artifacts:
  linux/amd64:
    path: app-linux-amd64.tar.gz
    sha256: abc123
validationManifestSHA256: %s
validationCatalogSHA256: %s
`, testSHA256(manifestData), testSHA256(catalogData)))

	requests := map[string]int{}
	var requestsMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("sourceRef"); got != "ref" {
			t.Errorf("sourceRef query = %q, want ref", got)
		}
		requestsMu.Lock()
		requests[r.URL.Path]++
		requestsMu.Unlock()
		setYAMLContentType(w)
		switch r.URL.Path {
		case "/snapshots/provider-release.yaml":
			_, _ = w.Write(metadataData)
		case "/snapshots/validation-manifest.yaml":
			_, _ = w.Write(manifestData)
		case "/snapshots/validation-catalog.yaml":
			_, _ = w.Write(catalogData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	bundle, resolved, assets, err := fetchProviderReleaseBundle(context.Background(), srv.Client(), srv.URL+"/snapshots/provider-release.yaml?sourceRef=ref", "")
	if err != nil {
		t.Fatalf("fetchProviderReleaseBundle: %v", err)
	}
	if resolved != srv.URL+"/snapshots/provider-release.yaml?sourceRef=ref" {
		t.Fatalf("resolved metadata location = %q", resolved)
	}
	if len(assets) != 0 {
		t.Fatalf("gitHubReleaseAssets = %#v, want none", assets)
	}
	if bundle.Metadata.Schema != providerrelease.SchemaName || bundle.Metadata.SchemaVersion != providerrelease.SchemaVersion {
		t.Fatalf("metadata schema = %q version %d, want provider release v1", bundle.Metadata.Schema, bundle.Metadata.SchemaVersion)
	}
	if bundle.Metadata.Runtime != providerrelease.RuntimeExecutable {
		t.Fatalf("metadata runtime = %q, want executable", bundle.Metadata.Runtime)
	}
	if bundle.Manifest.Source != "github.com/acme/providers/app" || bundle.Catalog == nil || bundle.Catalog.Name != "app" {
		t.Fatalf("bundle manifest source = %q catalog = %#v", bundle.Manifest.Source, bundle.Catalog)
	}
	for _, path := range []string{"/snapshots/provider-release.yaml", "/snapshots/validation-manifest.yaml", "/snapshots/validation-catalog.yaml"} {
		requestsMu.Lock()
		got := requests[path]
		requestsMu.Unlock()
		if got != 1 {
			t.Fatalf("request count for %s = %d, want 1", path, got)
		}
	}
}

func TestFetchProviderReleaseBundleRejectsSidecarDigestMismatch(t *testing.T) {
	t.Parallel()

	manifestData := []byte(`kind: app
source: github.com/acme/providers/app
version: 1.0.0
spec: {}
`)
	metadataData := []byte(`package: github.com/acme/providers/app
kind: app
version: 1.0.0
artifacts:
  linux/amd64:
    path: app-linux-amd64.tar.gz
    sha256: abc123
validationManifestSHA256: 0000000000000000000000000000000000000000000000000000000000000000
`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setYAMLContentType(w)
		switch r.URL.Path {
		case "/snapshots/provider-release.yaml":
			_, _ = w.Write(metadataData)
		case "/snapshots/validation-manifest.yaml":
			_, _ = w.Write(manifestData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, _, _, err := fetchProviderReleaseBundle(context.Background(), srv.Client(), srv.URL+"/snapshots/provider-release.yaml?sourceRef=ref", "")
	if err == nil || !strings.Contains(err.Error(), "sidecar digest mismatch") {
		t.Fatalf("fetchProviderReleaseBundle error = %v, want sidecar digest mismatch", err)
	}
}

func testSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}
