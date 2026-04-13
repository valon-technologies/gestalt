package providerpkg

import (
	"path/filepath"
	"testing"
)

func TestGoModulePathFromFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), []byte("module example.com/provider\n\ngo 1.26\n"), 0o644)

	got, err := goModulePathFromFile(root)
	if err != nil {
		t.Fatalf("goModulePathFromFile: %v", err)
	}
	if got != "example.com/provider" {
		t.Fatalf("module path = %q, want %q", got, "example.com/provider")
	}
}

func TestGoModulePathFromFileStripsComments(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), []byte("module example.com/provider // comment\n"), 0o644)

	got, err := goModulePathFromFile(root)
	if err != nil {
		t.Fatalf("goModulePathFromFile: %v", err)
	}
	if got != "example.com/provider" {
		t.Fatalf("module path = %q, want %q", got, "example.com/provider")
	}
}
