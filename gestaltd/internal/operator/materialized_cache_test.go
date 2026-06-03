package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializedCachePreservesEmptyDirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := writeMaterializedCacheUISource(t, dir, "source")
	if err := os.MkdirAll(filepath.Join(source, "assets", "empty"), 0o755); err != nil {
		t.Fatalf("create empty asset dir: %v", err)
	}

	req := materializedCacheUIRequest(dir, "alpha", "dest")
	cache := materializedCache{dir: filepath.Join(dir, "cache")}
	if _, _, _, err := cache.Put(req, source); err != nil {
		t.Fatalf("Put: %v", err)
	}
	restore, err := cache.Restore(req)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restore == nil || restore.Result != materializedCacheHit {
		t.Fatalf("Restore result = %#v, want hit", restore)
	}
	defer func() { _ = restore.cleanup() }()
	if err := restore.commit(); err != nil {
		t.Fatalf("commit restore: %v", err)
	}
	if info, err := os.Stat(filepath.Join(req.DestinationDir, "assets", "empty")); err != nil || !info.IsDir() {
		t.Fatalf("restored empty asset dir stat = %v, %v; want directory", info, err)
	}
}

func TestMaterializedCacheRestoresSharedArchiveForDifferentSubjects(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := writeMaterializedCacheUISource(t, dir, "source")
	cache := materializedCache{dir: filepath.Join(dir, "cache")}
	req := materializedCacheUIRequest(dir, "alpha", "dest-alpha")
	if _, _, _, err := cache.Put(req, source); err != nil {
		t.Fatalf("Put: %v", err)
	}

	sharedReq := materializedCacheUIRequest(dir, "beta", "dest-beta")
	restore, err := cache.Restore(sharedReq)
	if err != nil {
		t.Fatalf("Restore shared archive: %v", err)
	}
	if restore == nil || restore.Result != materializedCacheHit {
		t.Fatalf("shared Restore result = %#v, want hit", restore)
	}
	defer func() { _ = restore.cleanup() }()
}

func writeMaterializedCacheUISource(t *testing.T, dir, name string) string {
	t.Helper()

	source := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(source, "assets"), 0o755); err != nil {
		t.Fatalf("create UI source assets: %v", err)
	}
	manifest := []byte("kind: ui\nsource: github.com/acme/pkg/ui\nversion: 0.0.1\ndisplayName: Test UI\ndescription: Test UI\nspec:\n  assetRoot: assets\n  routes:\n    - path: /\n      allowedRoles: [viewer]\n")
	if err := os.WriteFile(filepath.Join(source, "manifest.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("write UI manifest: %v", err)
	}
	return source
}

func materializedCacheUIRequest(dir, name, destName string) materializedCacheRequest {
	return materializedCacheRequest{
		Subject:        `ui provider "` + name + `"`,
		Kind:           "ui",
		Name:           name,
		SourceKind:     syncArtifactSourceRemoteArchive,
		ArchiveSHA256:  materializedCacheTestSHA256Hex([]byte("archive")),
		ResolvedKey:    "darwin/arm64",
		Platform:       "darwin/arm64",
		Package:        "github.com/acme/pkg/ui",
		Version:        "0.0.1",
		DestinationDir: filepath.Join(dir, destName),
	}
}

func TestMaterializedCacheTreatsUnsafeMetadataAsInvalid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := materializedCache{dir: filepath.Join(dir, "cache")}
	req := materializedCacheRequest{
		Subject:        `provider "alpha"`,
		Kind:           "app",
		Name:           "alpha",
		SourceKind:     syncArtifactSourceRemoteArchive,
		ArchiveSHA256:  materializedCacheTestSHA256Hex([]byte("archive")),
		ResolvedKey:    "darwin/arm64",
		Platform:       "darwin/arm64",
		Package:        "github.com/acme/pkg/app",
		Version:        "0.0.1",
		DestinationDir: filepath.Join(dir, "dest"),
	}
	key, eligible, err := materializedCacheKeyForRequest(req)
	if err != nil || !eligible {
		t.Fatalf("materializedCacheKeyForRequest eligible=%t err=%v", eligible, err)
	}
	entryDir := cache.entryDir(key)
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		t.Fatalf("create entry dir: %v", err)
	}
	entry := materializedCacheEntry{
		Schema:              materializedCacheSchema,
		SchemaVersion:       materializedCacheSchemaVersion,
		MaterializerVersion: materializedCacheVersion,
		ArchiveSHA256:       key.ArchiveSHA256,
		Platform:            key.Platform,
		ResolvedKeyHash:     key.ResolvedKeyHash,
		OutputDigest:        "invalid",
		Files: []materializedCacheFile{{
			Path: "../escape",
			Type: "file",
		}},
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(entryDir, materializedCacheEntryFile), data, 0o644); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	restore, err := cache.Restore(req)
	if err != nil {
		t.Fatalf("Restore unsafe entry error = %v, want repairable invalid result", err)
	}
	if restore == nil || restore.Result != materializedCacheInvalid {
		t.Fatalf("Restore unsafe entry = %#v, want invalid", restore)
	}
}

func materializedCacheTestSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
