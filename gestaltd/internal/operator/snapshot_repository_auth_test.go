package operator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"golang.org/x/oauth2"
)

func TestSnapshotRepositoryHTTPClientUsesADCOnlyForGCS(t *testing.T) {
	t.Parallel()

	authorizationByHost := map[string]string{}
	lifecycle := &Lifecycle{
		httpClient: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			authorizationByHost[req.URL.Host] = req.Header.Get("Authorization")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    req,
			}, nil
		})},
		snapshotGCSTokenSource: func(context.Context) (oauth2.TokenSource, error) {
			return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "adc-token"}), nil
		},
	}
	cfg := &config.Config{ProviderSnapshotRepositories: map[string]config.ProviderSnapshotRepositoryConfig{
		"valon": {
			URL:  "https://storage.googleapis.com/private-snapshots",
			Auth: config.ProviderSnapshotRepositoryAuthGCPADC,
		},
	}}
	entry := &config.ProviderEntry{Source: config.NewGitSource(config.GitSourceDef{
		ArtifactRepository: "valon",
		Materialization:    gitMaterializationSnapshot,
	})}

	client, err := lifecycle.snapshotRepositoryHTTPClient(context.Background(), cfg, entry)
	if err != nil {
		t.Fatalf("snapshotRepositoryHTTPClient() error = %v", err)
	}
	for _, rawURL := range []string{
		"https://storage.googleapis.com/private-snapshots/provider-release.yaml",
		"https://archives.example.test/provider.tar.gz",
	} {
		response, err := client.Get(rawURL)
		if err != nil {
			t.Fatalf("GET %s: %v", rawURL, err)
		}
		_ = response.Body.Close()
	}

	if got := authorizationByHost["storage.googleapis.com"]; got != "Bearer adc-token" {
		t.Fatalf("GCS Authorization = %q", got)
	}
	if got := authorizationByHost["archives.example.test"]; got != "" {
		t.Fatalf("non-GCS Authorization = %q, want empty", got)
	}
}

func TestSnapshotRepositoryHTTPClientLeavesLegacyRepositoriesUnauthenticated(t *testing.T) {
	t.Parallel()

	var authorization string
	lifecycle := &Lifecycle{
		httpClient: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			authorization = req.Header.Get("Authorization")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    req,
			}, nil
		})},
		snapshotGCSTokenSource: func(context.Context) (oauth2.TokenSource, error) {
			return nil, fmt.Errorf("ADC must not be loaded")
		},
	}
	cfg := &config.Config{ProviderSnapshotRepositories: map[string]config.ProviderSnapshotRepositoryConfig{
		"valon": {URL: "https://storage.googleapis.com/public-snapshots"},
	}}
	entry := &config.ProviderEntry{Source: config.NewGitSource(config.GitSourceDef{
		ArtifactRepository: "valon",
		Materialization:    gitMaterializationSnapshot,
	})}

	client, err := lifecycle.snapshotRepositoryHTTPClient(context.Background(), cfg, entry)
	if err != nil {
		t.Fatalf("snapshotRepositoryHTTPClient() error = %v", err)
	}
	response, err := client.Get("https://storage.googleapis.com/public-snapshots/provider-release.yaml")
	if err != nil {
		t.Fatalf("GET legacy snapshot: %v", err)
	}
	_ = response.Body.Close()
	if authorization != "" {
		t.Fatalf("Authorization = %q, want empty", authorization)
	}
}
