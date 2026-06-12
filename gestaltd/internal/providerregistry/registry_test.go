package providerregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testPackage = "github.com/example/providers/app/demo"

const mirroredIndex = `schema: gestaltd-provider-index
schemaVersion: 1
packages:
  github.com/example/providers/app/demo:
    versions:
      "1.0.0":
        metadata: "https://example.com/releases/demo/v1.0.0/provider-release.yaml"
        kind: "app"
        runtime: "executable"
`

const divergentIndex = `schema: gestaltd-provider-index
schemaVersion: 1
packages:
  github.com/example/providers/app/demo:
    versions:
      "1.0.0":
        metadata: "https://other.example.com/releases/demo/v1.0.0/provider-release.yaml"
        kind: "app"
        runtime: "executable"
`

func newIndexServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, body := range map[string]string{
		"/canonical.yaml": mirroredIndex,
		"/mirror.yaml":    mirroredIndex,
		"/divergent.yaml": divergentIndex,
	} {
		body := body
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		})
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// Two repositories that resolve a package to the same version and metadata
// URL are aliases of one index (e.g. a legacy explicit "valon" entry next to
// the built-in default) — resolution must succeed and prefer the first
// repository in order, not report an ambiguity.
func TestResolveCollapsesMirroredRepositories(t *testing.T) {
	t.Parallel()
	server := newIndexServer(t)
	resolver := &Resolver{Client: server.Client()}
	resolved, err := resolver.Resolve(context.Background(), ResolveRequest{
		Package: testPackage,
		Repositories: []NamedRepository{
			{Name: "gestalt", URL: server.URL + "/canonical.yaml"},
			{Name: "valon", URL: server.URL + "/mirror.yaml"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error for mirrored repositories: %v", err)
	}
	if resolved.RepositoryName != "gestalt" {
		t.Fatalf("Resolve picked %q, want the first repository %q", resolved.RepositoryName, "gestalt")
	}
	if resolved.Version != "1.0.0" {
		t.Fatalf("Resolve picked version %q, want %q", resolved.Version, "1.0.0")
	}
}

// Repositories whose answers genuinely diverge stay ambiguous: picking one
// silently would mean resolving a package from the wrong publisher.
func TestResolveRejectsDivergentRepositories(t *testing.T) {
	t.Parallel()
	server := newIndexServer(t)
	resolver := &Resolver{Client: server.Client()}
	_, err := resolver.Resolve(context.Background(), ResolveRequest{
		Package: testPackage,
		Repositories: []NamedRepository{
			{Name: "gestalt", URL: server.URL + "/canonical.yaml"},
			{Name: "fork", URL: server.URL + "/divergent.yaml"},
		},
	})
	if err == nil {
		t.Fatal("Resolve succeeded for divergent repositories, want ambiguity error")
	}
	if !strings.Contains(err.Error(), "multiple repositories") {
		t.Fatalf("Resolve error = %q, want it to name the multiple-repositories ambiguity", err)
	}
}

// An explicit --repo selection bypasses the ambiguity question entirely.
func TestResolveHonorsExplicitRepositorySelection(t *testing.T) {
	t.Parallel()
	server := newIndexServer(t)
	resolver := &Resolver{Client: server.Client()}
	resolved, err := resolver.Resolve(context.Background(), ResolveRequest{
		Package:        testPackage,
		RepositoryName: "fork",
		Repositories: []NamedRepository{
			{Name: "gestalt", URL: server.URL + "/canonical.yaml"},
			{Name: "fork", URL: server.URL + "/divergent.yaml"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve with explicit repository returned error: %v", err)
	}
	if resolved.RepositoryName != "fork" {
		t.Fatalf("Resolve picked %q, want explicitly selected %q", resolved.RepositoryName, "fork")
	}
}
