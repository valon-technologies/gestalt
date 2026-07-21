package operator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryOnlyAppLockAndSyncSkipArtifacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "gestalt.yaml")
	lockPath := filepath.Join(dir, LockfileName)
	artifactsDir := filepath.Join(dir, "artifacts")
	configYAML := fmt.Sprintf(`
apiVersion: gestaltd.config/v8
%s
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: test-registry
apps:
  g-issues:
    source:
      registry: toolshed
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), filepath.ToSlash(artifactsDir))
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	lifecycle := NewLifecycle()
	lock, err := lifecycle.LockAtPaths([]string{configPath}, lockPath, artifactsDir)
	if err != nil {
		t.Fatalf("LockAtPaths: %v", err)
	}
	entry := lock.Providers.App["g-issues"]
	if entry.Source != "registry" || entry.SourceRef == nil ||
		entry.SourceRef.Type != "registry" || entry.SourceRef.ResolvedGestaltRef != "toolshed" {
		t.Fatalf("lock entry = %#v", entry)
	}
	if len(entry.Archives) != 0 {
		t.Fatalf("archives = %#v", entry.Archives)
	}
	if err := lifecycle.SyncAtPathsOptions([]string{configPath}, lockPath, artifactsDir, SyncOptions{Locked: true}); err != nil {
		t.Fatalf("SyncAtPathsOptions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(artifactsDir, PreparedProvidersDir, "g-issues")); !os.IsNotExist(err) {
		t.Fatalf("registry app artifact was materialized: %v", err)
	}
}
