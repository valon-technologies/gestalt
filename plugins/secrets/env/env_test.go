package env

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/core"
)

func TestProvider(t *testing.T) {
	t.Setenv("GENERIC_SECRET", "hello")
	t.Setenv("APP_DB_PASSWORD", "pw123")

	t.Run("resolves normalized names with and without prefixes", func(t *testing.T) {
		ctx := context.Background()

		base := &Provider{}
		val, err := base.GetSecret(ctx, "generic-secret")
		if err != nil {
			t.Fatalf("GetSecret(base): %v", err)
		}
		if val != "hello" {
			t.Fatalf("GetSecret(base) = %q, want hello", val)
		}

		prefixed := &Provider{prefix: "APP_"}
		val, err = prefixed.GetSecret(ctx, "db-password")
		if err != nil {
			t.Fatalf("GetSecret(prefixed): %v", err)
		}
		if val != "pw123" {
			t.Fatalf("GetSecret(prefixed) = %q, want pw123", val)
		}
	})

	t.Run("returns ErrSecretNotFound for missing env vars", func(t *testing.T) {
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
