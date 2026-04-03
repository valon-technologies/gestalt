package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestProvider(t *testing.T) {
	t.Parallel()

	t.Run("reads secret from file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "my-secret"), []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}

		p := &Provider{dir: dir}
		val, err := p.GetSecret(context.Background(), "my-secret")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if val != "hello" {
			t.Errorf("got %q, want %q", val, "hello")
		}
	})

	t.Run("trims trailing newline", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "newline-secret"), []byte("value\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		p := &Provider{dir: dir}
		val, err := p.GetSecret(context.Background(), "newline-secret")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if val != "value" {
			t.Errorf("got %q, want %q", val, "value")
		}
	})

	t.Run("returns ErrSecretNotFound for missing file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := &Provider{dir: dir}
		_, err := p.GetSecret(context.Background(), "nonexistent")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, core.ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound, got: %v", err)
		}
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := &Provider{dir: dir}
		_, err := p.GetSecret(context.Background(), "../../etc/shadow")
		if err == nil {
			t.Fatal("expected error for path traversal, got nil")
		}
		if errors.Is(err, core.ErrSecretNotFound) {
			t.Error("path traversal should not return ErrSecretNotFound")
		}
	})
}
