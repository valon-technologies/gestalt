package providerpkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePackageTempBaseDirCreatesMissingCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missing := filepath.Join(root, "missing")

	got, err := resolvePackageTempBaseDir([]string{missing})
	if err != nil {
		t.Fatalf("resolvePackageTempBaseDir() error: %v", err)
	}
	if got != missing {
		t.Fatalf("resolvePackageTempBaseDir() = %q, want %q", got, missing)
	}
	info, err := os.Stat(missing)
	if err != nil {
		t.Fatalf("stat created directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("created candidate is not a directory")
	}
}

func TestResolvePackageTempBaseDirSkipsFileCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fileCandidate := filepath.Join(root, "candidate-file")
	if err := os.WriteFile(fileCandidate, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("write file candidate: %v", err)
	}
	dirCandidate := filepath.Join(root, "candidate-dir")

	got, err := resolvePackageTempBaseDir([]string{fileCandidate, dirCandidate})
	if err != nil {
		t.Fatalf("resolvePackageTempBaseDir() error: %v", err)
	}
	if got != dirCandidate {
		t.Fatalf("resolvePackageTempBaseDir() = %q, want %q", got, dirCandidate)
	}
	info, err := os.Stat(dirCandidate)
	if err != nil {
		t.Fatalf("stat created directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("created fallback candidate is not a directory")
	}
}
