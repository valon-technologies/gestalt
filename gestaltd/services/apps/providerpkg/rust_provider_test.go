package providerpkg

import (
	"os"
	"path/filepath"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestNewRustWrapperProjectCopiesCargoLock(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lockContent := []byte("# locked dependencies\n")
	if err := os.WriteFile(filepath.Join(root, rustLockFile), lockContent, 0o644); err != nil {
		t.Fatalf("write Cargo.lock: %v", err)
	}

	wrapperDir, cleanup, err := newRustWrapperProject(root, "example-provider", "example", providermanifestv1.KindApp)
	if err != nil {
		t.Fatalf("newRustWrapperProject: %v", err)
	}
	defer cleanup()

	got, err := os.ReadFile(filepath.Join(wrapperDir, rustLockFile))
	if err != nil {
		t.Fatalf("read wrapper Cargo.lock: %v", err)
	}
	if string(got) != string(lockContent) {
		t.Fatalf("wrapper Cargo.lock = %q, want %q", got, lockContent)
	}
}

func TestNewRustWrapperProjectAllowsMissingCargoLock(t *testing.T) {
	t.Parallel()

	wrapperDir, cleanup, err := newRustWrapperProject(t.TempDir(), "example-provider", "example", providermanifestv1.KindApp)
	if err != nil {
		t.Fatalf("newRustWrapperProject: %v", err)
	}
	defer cleanup()

	if _, err := os.Stat(filepath.Join(wrapperDir, rustLockFile)); !os.IsNotExist(err) {
		t.Fatalf("wrapper Cargo.lock stat error = %v, want not exist", err)
	}
}
