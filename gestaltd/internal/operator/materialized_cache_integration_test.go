package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestSyncPrefetchesRequestedRemoteMaterializedCache(t *testing.T) {
	dir := t.TempDir()
	packageSource := "github.com/acme/tools/alpha"
	version := "1.2.3"
	archivePath := buildV2Archive(t, dir, packageSource, version, "provider-package-alpha")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSum := sha256.Sum256(archiveData)
	archiveSHAHex := hex.EncodeToString(archiveSum[:])

	var archiveCount atomic.Int64
	indexPath := "/provider-index.yaml"
	metadataPath := "/providers/alpha/v1.2.3/provider-release.yaml"
	archiveURLPath := "/providers/alpha/v1.2.3/alpha.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case indexPath:
			index := fmt.Sprintf(`
schema: gestaltd-provider-index
schemaVersion: 1
packages:
  github.com/acme/tools/alpha:
    versions:
      %s:
        metadata: providers/alpha/v1.2.3/provider-release.yaml
        kind: app
        runtime: executable
        platforms:
          - %s
`, version, providerpkg.CurrentPlatformString())
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(index))
		case metadataPath:
			metadata := providerReleaseMetadataFixture{
				Package:     packageSource,
				Kind:        providermanifestv1.KindApp,
				Version:     version,
				ArchivePath: archivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archiveURLPath),
						SHA256: archiveSHAHex,
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archiveURLPath:
			archiveCount.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	lockfilePath := filepath.Join(dir, LockfileName)
	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"providerRepositories:",
		"  local:",
		"    url: " + srv.URL + indexPath,
		strings.TrimSuffix(requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), "\n"),
		"apps:",
		"  alpha:",
		"    source:",
		"      repo: local",
		"      package: " + packageSource,
		"      version: \">= 1.0.0, < 2.0.0\"",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.PrepareAtPaths([]string{configPath}, lockfilePath, artifactsDir); err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}

	remote := newFakeMaterializedCacheRemote()
	const remoteURL = "gs://cache-bucket/prefix"
	oldFactory := newMaterializedCacheRemote
	newMaterializedCacheRemote = func(raw string) (materializedCacheRemote, error) {
		if raw != remoteURL {
			return nil, fmt.Errorf("remote url = %q, want %q", raw, remoteURL)
		}
		return remote, nil
	}
	t.Cleanup(func() { newMaterializedCacheRemote = oldFactory })
	t.Setenv(materializedCacheRemoteEnv, remoteURL)

	appRoot := filepath.Join(artifactsDir, "providers", "alpha")
	if err := os.RemoveAll(appRoot); err != nil {
		t.Fatalf("remove prepared app before seed sync: %v", err)
	}
	archiveBeforeSeed := archiveCount.Load()
	seedCacheDir := filepath.Join(dir, "seed-cache")
	if err := lc.SyncAtPathsOptions([]string{configPath}, lockfilePath, artifactsDir, SyncOptions{CacheDir: seedCacheDir}); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	if got := archiveCount.Load(); got != archiveBeforeSeed+1 {
		t.Fatalf("archive requests after seed sync = %d, want %d", got, archiveBeforeSeed+1)
	}
	if remote.puts != 1 {
		t.Fatalf("remote puts after seed sync = %d, want 1", remote.puts)
	}

	if err := os.RemoveAll(appRoot); err != nil {
		t.Fatalf("remove prepared app before prefetch sync: %v", err)
	}
	prefetchMetrics := NewSyncMetricsRecorder()
	archiveBeforePrefetch := archiveCount.Load()
	prefetchCacheDir := filepath.Join(dir, "prefetch-cache")
	if err := lc.SyncAtPathsOptions([]string{configPath}, lockfilePath, artifactsDir, SyncOptions{
		CacheDir:      prefetchCacheDir,
		Observability: SyncObservability{Recorder: prefetchMetrics},
	}); err != nil {
		t.Fatalf("prefetch sync: %v", err)
	}
	if got := archiveCount.Load(); got != archiveBeforePrefetch {
		t.Fatalf("archive requests after remote prefetch sync = %d, want %d", got, archiveBeforePrefetch)
	}
	prefetch := prefetchMetrics.Snapshot().Cache.Prefetch
	if prefetch.Requests == 0 || prefetch.RemoteHits != 1 || prefetch.Failures != 0 || prefetch.Bytes == 0 {
		t.Fatalf("cache.prefetch metrics = %+v, want one remote hit and no failures", prefetch)
	}

	remoteGetsBeforeFreshSync := remote.gets
	archiveBeforeFreshSync := archiveCount.Load()
	freshMetrics := NewSyncMetricsRecorder()
	if err := lc.SyncAtPathsOptions([]string{configPath}, lockfilePath, artifactsDir, SyncOptions{
		CacheDir:      filepath.Join(dir, "fresh-cache"),
		Observability: SyncObservability{Recorder: freshMetrics},
	}); err != nil {
		t.Fatalf("fresh sync: %v", err)
	}
	if got := archiveCount.Load(); got != archiveBeforeFreshSync {
		t.Fatalf("archive requests after fresh sync = %d, want %d", got, archiveBeforeFreshSync)
	}
	if got := remote.gets; got != remoteGetsBeforeFreshSync {
		t.Fatalf("remote cache reads after fresh sync = %d, want %d", got, remoteGetsBeforeFreshSync)
	}
	if prefetch := freshMetrics.Snapshot().Cache.Prefetch; prefetch.Requests != 0 || prefetch.RemoteHits != 0 || prefetch.LocalHits != 0 {
		t.Fatalf("fresh sync cache.prefetch metrics = %+v, want no prefetch work", prefetch)
	}
}

func assertCheckSyncDoesNotPopulateMaterializedCache(t *testing.T, lc *Lifecycle, configPath, lockfilePath, artifactsDir, cacheDir string, resetArtifacts func() error) {
	t.Helper()

	if err := resetArtifacts(); err != nil {
		t.Fatalf("reset artifacts before check sync: %v", err)
	}
	if err := lc.CheckSyncAtPathsOptions([]string{configPath}, lockfilePath, artifactsDir, SyncOptions{CacheDir: cacheDir}); err == nil {
		t.Fatal("CheckSyncAtPathsOptions with cache dir error = nil, want stale artifact error")
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("cache dir after check sync stat err = %v, want not exist", err)
	}
}

func assertRemoteMaterializedCacheRoundTrip(
	t *testing.T,
	lc *Lifecycle,
	configPath string,
	lockfilePath string,
	artifactsDir string,
	cacheDir string,
	archiveSHAHex string,
	archiveCount *atomic.Int64,
	resetArtifacts func() error,
	afterMiss func(),
) {
	t.Helper()

	if err := resetArtifacts(); err != nil {
		t.Fatalf("reset artifacts before cache miss: %v", err)
	}
	archiveBeforeCacheMiss := archiveCount.Load()
	coldMetrics := NewSyncMetricsRecorder()
	if err := lc.SyncAtPathsOptions([]string{configPath}, lockfilePath, artifactsDir, SyncOptions{CacheDir: cacheDir, Observability: SyncObservability{Recorder: coldMetrics}}); err != nil {
		if afterMiss != nil {
			afterMiss()
		}
		t.Fatalf("SyncAtPathsOptions cache miss: %v", err)
	}
	if afterMiss != nil {
		afterMiss()
	}
	if got := archiveCount.Load(); got != archiveBeforeCacheMiss+1 {
		t.Fatalf("archive request count after cache miss = %d, want %d", got, archiveBeforeCacheMiss+1)
	}
	if entryPath := materializedCacheEntryPathForTest(t, cacheDir, archiveSHAHex); entryPath == "" {
		t.Fatalf("materialized cache entry path is empty; metrics=%+v", coldMetrics.Snapshot().Cache)
	}

	if err := resetArtifacts(); err != nil {
		t.Fatalf("reset artifacts before cache hit: %v", err)
	}
	archiveBeforeCacheHit := archiveCount.Load()
	if err := lc.SyncAtPathsOptions([]string{configPath}, lockfilePath, artifactsDir, SyncOptions{CacheDir: cacheDir}); err != nil {
		t.Fatalf("SyncAtPathsOptions cache hit: %v", err)
	}
	if got := archiveCount.Load(); got != archiveBeforeCacheHit {
		t.Fatalf("archive request count after cache hit = %d, want %d", got, archiveBeforeCacheHit)
	}
}

func assertRemoteMaterializedCacheRepair(
	t *testing.T,
	lc *Lifecycle,
	configPath string,
	lockfilePath string,
	artifactsDir string,
	cacheDir string,
	archiveSHAHex string,
	archiveCount *atomic.Int64,
	resetArtifacts func() error,
	afterRepair func(),
) {
	t.Helper()

	entryPath := materializedCacheEntryPathForTest(t, cacheDir, archiveSHAHex)
	rootDir := filepath.Join(filepath.Dir(entryPath), materializedCacheRootDir)
	corruptPath := firstMaterializedCacheFileForTest(t, rootDir)
	if err := os.WriteFile(corruptPath, []byte("corrupt cached prepared output"), 0o644); err != nil {
		t.Fatalf("write corrupt materialized cache file: %v", err)
	}
	if err := resetArtifacts(); err != nil {
		t.Fatalf("reset artifacts before cache repair: %v", err)
	}
	archiveBeforeCacheRepair := archiveCount.Load()
	if err := lc.SyncAtPathsOptions([]string{configPath}, lockfilePath, artifactsDir, SyncOptions{CacheDir: cacheDir}); err != nil {
		if afterRepair != nil {
			afterRepair()
		}
		t.Fatalf("SyncAtPathsOptions cache repair: %v", err)
	}
	if afterRepair != nil {
		afterRepair()
	}
	if got := archiveCount.Load(); got != archiveBeforeCacheRepair+1 {
		t.Fatalf("archive request count after cache repair = %d, want %d", got, archiveBeforeCacheRepair+1)
	}
}

func materializedCacheEntryPathForTest(t *testing.T, cacheDir, archiveSHAHex string) string {
	t.Helper()

	sha, ok := canonicalArchiveSHA256(archiveSHAHex)
	if !ok {
		t.Fatalf("archive SHA %q is not cacheable", archiveSHAHex)
	}
	pattern := filepath.Join(cacheDir, materializedCacheBucketVersion, "*", "sha256", sha[:2], sha, "*", materializedCacheEntryFile)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob materialized cache entry: %v", err)
	}
	if len(matches) != 1 {
		all, _ := filepath.Glob(filepath.Join(cacheDir, materializedCacheBucketVersion, "*", "sha256", "*", "*", "*", materializedCacheEntryFile))
		t.Fatalf("materialized cache entries for %s = %d, want 1 (%v); all entries=%v", sha, len(matches), matches, all)
	}
	return matches[0]
}

func firstMaterializedCacheFileForTest(t *testing.T, rootDir string) string {
	t.Helper()

	var first string
	if err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if first == "" && !d.IsDir() {
			first = path
		}
		return nil
	}); err != nil {
		t.Fatalf("walk materialized cache root: %v", err)
	}
	if first == "" {
		t.Fatalf("no materialized cache files under %s", rootDir)
	}
	return first
}
