package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/core"
)

func TestRun_TopLevelArray(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"site_id": "s1", "site_name": "Alpha", "region": "us-east"},
			{"site_id": "s2", "site_name": "Beta", "region": "eu-west"},
			{"site_id": "s3", "site_name": "Gamma", "region": "ap-south"}
		]`))
	}))
	defer srv.Close()

	cfg := &core.DiscoveryConfig{
		URL:      srv.URL,
		IDPath:   "site_id",
		NamePath: "site_name",
		MetadataMapping: map[string]string{
			"region": "region",
		},
	}

	candidates, err := Run(context.Background(), cfg, srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("got %d candidates, want 3", len(candidates))
	}

	if candidates[0].ID != "s1" {
		t.Errorf("candidates[0].ID = %q, want %q", candidates[0].ID, "s1")
	}
	if candidates[0].Name != "Alpha" {
		t.Errorf("candidates[0].Name = %q, want %q", candidates[0].Name, "Alpha")
	}
	if candidates[0].Metadata["region"] != "us-east" {
		t.Errorf("candidates[0].Metadata[region] = %q, want %q", candidates[0].Metadata["region"], "us-east")
	}

	if candidates[2].ID != "s3" {
		t.Errorf("candidates[2].ID = %q, want %q", candidates[2].ID, "s3")
	}
	if candidates[2].Name != "Gamma" {
		t.Errorf("candidates[2].Name = %q, want %q", candidates[2].Name, "Gamma")
	}
}

func TestRun_NestedItemsPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"resources": [
					{"rid": "r1", "label": "Prod"},
					{"rid": "r2", "label": "Staging"}
				]
			}
		}`))
	}))
	defer srv.Close()

	cfg := &core.DiscoveryConfig{
		URL:       srv.URL,
		ItemsPath: "data.resources",
		IDPath:    "rid",
		NamePath:  "label",
	}

	candidates, err := Run(context.Background(), cfg, srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("got %d candidates, want 2", len(candidates))
	}
	if candidates[0].ID != "r1" {
		t.Errorf("candidates[0].ID = %q, want %q", candidates[0].ID, "r1")
	}
	if candidates[1].Name != "Staging" {
		t.Errorf("candidates[1].Name = %q, want %q", candidates[1].Name, "Staging")
	}
}

func TestRun_EmptyArray(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	cfg := &core.DiscoveryConfig{
		URL:    srv.URL,
		IDPath: "id",
	}

	candidates, err := Run(context.Background(), cfg, srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("got %d candidates, want 0", len(candidates))
	}
}

func TestRun_HTTP401(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "invalid token"}`))
	}))
	defer srv.Close()

	cfg := &core.DiscoveryConfig{
		URL:    srv.URL,
		IDPath: "id",
	}

	_, err := Run(context.Background(), cfg, srv.Client())
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
}

func TestRun_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	cfg := &core.DiscoveryConfig{
		URL:    srv.URL,
		IDPath: "id",
	}

	_, err := Run(context.Background(), cfg, srv.Client())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
