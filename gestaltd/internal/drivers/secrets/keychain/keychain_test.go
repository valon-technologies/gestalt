package keychain

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/zalando/go-keyring"
)

func init() {
	keyring.MockInit()
}

func TestProvider(t *testing.T) {
	t.Run("resolves secret from keychain", func(t *testing.T) {
		keyring.MockInit()
		if err := keyring.Set("test-service", "db-password", "s3cret"); err != nil {
			t.Fatal(err)
		}
		p := &Provider{service: "test-service"}
		val, err := p.GetSecret(context.Background(), "db-password")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if val != "s3cret" {
			t.Errorf("got %q, want %q", val, "s3cret")
		}
	})

	t.Run("returns ErrSecretNotFound for missing entry", func(t *testing.T) {
		keyring.MockInit()
		p := &Provider{service: "test-service"}
		_, err := p.GetSecret(context.Background(), "nonexistent")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, core.ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound, got: %v", err)
		}
	})

	t.Run("uses default service name", func(t *testing.T) {
		keyring.MockInit()
		if err := keyring.Set(defaultService, "api-key", "key123"); err != nil {
			t.Fatal(err)
		}
		p := &Provider{service: defaultService}
		val, err := p.GetSecret(context.Background(), "api-key")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if val != "key123" {
			t.Errorf("got %q, want %q", val, "key123")
		}
	})
}
