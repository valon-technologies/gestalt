package pluginhost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePluginTempBaseDirCreatesMissingCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missing := filepath.Join(root, "missing")

	got, err := resolvePluginTempBaseDir([]string{missing})
	if err != nil {
		t.Fatalf("resolvePluginTempBaseDir() error: %v", err)
	}
	if got != missing {
		t.Fatalf("resolvePluginTempBaseDir() = %q, want %q", got, missing)
	}
	info, err := os.Stat(missing)
	if err != nil {
		t.Fatalf("stat created directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("created candidate is not a directory")
	}
}

func TestResolvePluginTempBaseDirSkipsFileCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fileCandidate := filepath.Join(root, "candidate-file")
	if err := os.WriteFile(fileCandidate, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("write file candidate: %v", err)
	}
	dirCandidate := filepath.Join(root, "candidate-dir")

	got, err := resolvePluginTempBaseDir([]string{fileCandidate, dirCandidate})
	if err != nil {
		t.Fatalf("resolvePluginTempBaseDir() error: %v", err)
	}
	if got != dirCandidate {
		t.Fatalf("resolvePluginTempBaseDir() = %q, want %q", got, dirCandidate)
	}
	info, err := os.Stat(dirCandidate)
	if err != nil {
		t.Fatalf("stat created directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("created fallback candidate is not a directory")
	}
}
