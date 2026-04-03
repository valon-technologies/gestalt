package env

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestProvider(t *testing.T) {
	t.Run("resolves env var", func(t *testing.T) {
		t.Setenv("MY_SECRET", "hello")
		p := &Provider{}
		val, err := p.GetSecret(context.Background(), "my-secret")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if val != "hello" {
			t.Errorf("got %q, want %q", val, "hello")
		}
	})

	t.Run("applies prefix", func(t *testing.T) {
		t.Setenv("APP_DB_PASSWORD", "pw123")
		p := &Provider{prefix: "APP_"}
		val, err := p.GetSecret(context.Background(), "db-password")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if val != "pw123" {
			t.Errorf("got %q, want %q", val, "pw123")
		}
	})

	t.Run("returns ErrSecretNotFound for missing var", func(t *testing.T) {
		t.Parallel()
		p := &Provider{}
		_, err := p.GetSecret(context.Background(), "nonexistent-var-xyz")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, core.ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound, got: %v", err)
		}
	})

}
