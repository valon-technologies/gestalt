package operator

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func assertCheckSyncDoesNotPopulateMaterializedCache(t *testing.T, lc *Lifecycle, configPath, cacheDir string, resetArtifacts func() error) {
	t.Helper()

	if err := resetArtifacts(); err != nil {
		t.Fatalf("reset artifacts before check sync: %v", err)
	}
	if err := lc.CheckSyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{CacheDir: cacheDir}); err == nil {
		t.Fatal("CheckSyncAtPathsWithStatePathsOptions with cache dir error = nil, want stale artifact error")
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("cache dir after check sync stat err = %v, want not exist", err)
	}
}

func assertRemoteMaterializedCacheRoundTrip(
	t *testing.T,
	lc *Lifecycle,
	configPath string,
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
	if err := lc.SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{CacheDir: cacheDir, Observability: SyncObservability{Recorder: coldMetrics}}); err != nil {
		if afterMiss != nil {
			afterMiss()
		}
		t.Fatalf("SyncAtPathsWithStatePathsOptions cache miss: %v", err)
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
	if err := lc.SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{CacheDir: cacheDir}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePathsOptions cache hit: %v", err)
	}
	if got := archiveCount.Load(); got != archiveBeforeCacheHit {
		t.Fatalf("archive request count after cache hit = %d, want %d", got, archiveBeforeCacheHit)
	}
}

func assertRemoteMaterializedCacheRepair(
	t *testing.T,
	lc *Lifecycle,
	configPath string,
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
	if err := lc.SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{CacheDir: cacheDir}); err != nil {
		if afterRepair != nil {
			afterRepair()
		}
		t.Fatalf("SyncAtPathsWithStatePathsOptions cache repair: %v", err)
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
