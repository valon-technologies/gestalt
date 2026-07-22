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

func TestVersionSatisfiesFleetConstraint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version    string
		constraint string
		want       bool
	}{
		{version: "2.0.0", constraint: "^2.0.0", want: true},
		{version: "2.0.0-snapshot.gabc123", constraint: "^2.0.0", want: true},
		{version: "1.4.0-snapshot.gabc123", constraint: "^1.4.0", want: true},
		{version: "0.0.0-snapshot.gabc123", constraint: "^0.0.0", want: true},
		{version: "1.4.0-snapshot.gabc123", constraint: "^2.0.0", want: false},
		{version: "0.0.0-snapshot.gabc123", constraint: "^2.0.0", want: false},
		{version: "2.0.0-snapshot.gabc123", constraint: "", want: true},
		{version: "", constraint: "^2.0.0", want: false},
		{version: "2.0.0-alpha", constraint: "^2.0.0-beta", want: false},
		{version: "2.0.0-beta.1", constraint: ">=2.0.0-beta.2", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.version+"_"+tc.constraint, func(t *testing.T) {
			t.Parallel()
			if got := VersionSatisfiesFleetConstraint(tc.version, tc.constraint); got != tc.want {
				t.Fatalf("VersionSatisfiesFleetConstraint(%q, %q) = %v, want %v", tc.version, tc.constraint, got, tc.want)
			}
		})
	}

	t.Run("release_behavior_matches_VersionSatisfiesConstraint", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			version    string
			constraint string
		}{
			{version: "2.0.0", constraint: "^2.0.0"},
			{version: "1.4.0", constraint: "^1.0.0"},
			{version: "3.0.0", constraint: "^2.0.0"},
		} {
			if got := VersionSatisfiesFleetConstraint(tc.version, tc.constraint); got != VersionSatisfiesConstraint(tc.version, tc.constraint) {
				t.Fatalf("release version %q vs %q: fleet=%v constraint=%v", tc.version, tc.constraint, got, VersionSatisfiesConstraint(tc.version, tc.constraint))
			}
		}
	})
}

func TestResolveReportsEachRepositoryBeforeFetching(t *testing.T) {
	t.Parallel()
	server := newIndexServer(t)
	var fetched []string
	resolver := &Resolver{
		Client: server.Client(),
		OnRepositoryFetch: func(repo NamedRepository) {
			fetched = append(fetched, repo.Name)
		},
	}
	if _, err := resolver.Resolve(context.Background(), ResolveRequest{
		Package: testPackage,
		Repositories: []NamedRepository{
			{Name: "gestalt", URL: server.URL + "/canonical.yaml"},
			{Name: "fork", URL: server.URL + "/mirror.yaml"},
		},
	}); err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := strings.Join(fetched, ","), "gestalt,fork"; got != want {
		t.Fatalf("repositories reported = %q, want %q", got, want)
	}
}
