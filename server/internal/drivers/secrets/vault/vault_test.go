package vault

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	vaultapi "github.com/hashicorp/vault/api"

	"github.com/valon-technologies/gestalt/server/core"
)

func newTestProvider(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := vaultapi.DefaultConfig()
	cfg.Address = srv.URL
	client, err := vaultapi.NewClient(cfg)
	if err != nil {
		t.Fatalf("creating vault client: %v", err)
	}
	client.SetToken("test-token")
	return &Provider{client: client, mountPath: "secret"}
}

func TestProvider(t *testing.T) {
	t.Parallel()

	t.Run("resolves secret", func(t *testing.T) {
		t.Parallel()
		p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/secret/data/db-password" {
				http.NotFound(w, r)
				return
			}
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"value": "hunter2",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))

		val, err := p.GetSecret(context.Background(), "db-password")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if val != "hunter2" {
			t.Errorf("got %q, want %q", val, "hunter2")
		}
	})

	t.Run("returns ErrSecretNotFound for missing secret", func(t *testing.T) {
		t.Parallel()
		p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))

		_, err := p.GetSecret(context.Background(), "missing")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, core.ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound, got: %v", err)
		}
	})

	t.Run("returns error for missing value key", func(t *testing.T) {
		t.Parallel()
		p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"other_key": "something",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))

		_, err := p.GetSecret(context.Background(), "no-value")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("rejects slash in name", func(t *testing.T) {
		t.Parallel()
		p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("request should not have been made")
		}))

		_, err := p.GetSecret(context.Background(), "path/to/secret")
		if err == nil {
			t.Fatal("expected error for slash in name, got nil")
		}
	})

	t.Run("wraps server errors", func(t *testing.T) {
		t.Parallel()
		p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))

		_, err := p.GetSecret(context.Background(), "any-secret")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if errors.Is(err, core.ErrSecretNotFound) {
			t.Error("unexpected ErrSecretNotFound for server error")
		}
	})
}
