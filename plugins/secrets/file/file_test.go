package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/core"
)

func TestProvider(t *testing.T) {
	t.Parallel()

	t.Run("reads trimmed secrets and rejects invalid lookups", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "generic-secret"), []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "line-secret"), []byte("value\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		p := &Provider{dir: dir}

		val, err := p.GetSecret(context.Background(), "generic-secret")
		if err != nil {
			t.Fatalf("GetSecret(generic-secret): %v", err)
		}
		if val != "hello" {
			t.Fatalf("GetSecret(generic-secret) = %q, want hello", val)
		}

		val, err = p.GetSecret(context.Background(), "line-secret")
		if err != nil {
			t.Fatalf("GetSecret(line-secret): %v", err)
		}
		if val != "value" {
			t.Fatalf("GetSecret(line-secret) = %q, want value", val)
		}

		_, err = p.GetSecret(context.Background(), "nonexistent")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, core.ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound, got: %v", err)
		}

		_, err = p.GetSecret(context.Background(), "../../etc/shadow")
		if err == nil {
			t.Fatal("expected error for path traversal, got nil")
		}
		if errors.Is(err, core.ErrSecretNotFound) {
			t.Error("path traversal should not return ErrSecretNotFound")
		}
	})
}
