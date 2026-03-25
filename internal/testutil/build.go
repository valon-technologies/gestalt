package testutil

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// BuildGoBinary builds the given package from the repository root into a temp binary.
func BuildGoBinary(t *testing.T, pkgPath, binName string) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), binName)
	cmd := exec.Command("go", "build", "-o", bin, pkgPath)
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkgPath, err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
