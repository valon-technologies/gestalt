package operator

import (
	"os"
	"sync/atomic"
	"testing"
)

func assertCheckSyncDoesNotPopulateArchiveCache(t *testing.T, lc *Lifecycle, configPath, cacheDir string, resetArtifacts func() error) {
	t.Helper()

	if err := resetArtifacts(); err != nil {
		t.Fatalf("reset artifacts before check sync: %v", err)
	}
	if err := lc.CheckSyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{ArchiveCacheDir: cacheDir}); err == nil {
		t.Fatal("CheckSyncAtPathsWithStatePathsOptions with cache dir error = nil, want stale artifact error")
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("cache dir after check sync stat err = %v, want not exist", err)
	}
}

func assertRemoteArchiveCacheRoundTrip(
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
	if err := lc.SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{ArchiveCacheDir: cacheDir}); err != nil {
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
	cachePath := archiveCachePathForTest(t, cacheDir, archiveSHAHex)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache archive stat: %v", err)
	}

	if err := resetArtifacts(); err != nil {
		t.Fatalf("reset artifacts before cache hit: %v", err)
	}
	archiveBeforeCacheHit := archiveCount.Load()
	if err := lc.SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{ArchiveCacheDir: cacheDir}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePathsOptions cache hit: %v", err)
	}
	if got := archiveCount.Load(); got != archiveBeforeCacheHit {
		t.Fatalf("archive request count after cache hit = %d, want %d", got, archiveBeforeCacheHit)
	}
}

func assertRemoteArchiveCacheRepair(
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

	cachePath := archiveCachePathForTest(t, cacheDir, archiveSHAHex)
	if err := os.WriteFile(cachePath, []byte("corrupt cached archive"), 0o644); err != nil {
		t.Fatalf("write corrupt cache archive: %v", err)
	}
	if err := resetArtifacts(); err != nil {
		t.Fatalf("reset artifacts before cache repair: %v", err)
	}
	archiveBeforeCacheRepair := archiveCount.Load()
	if err := lc.SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{ArchiveCacheDir: cacheDir}); err != nil {
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

func archiveCachePathForTest(t *testing.T, cacheDir, archiveSHAHex string) string {
	t.Helper()

	sha, ok := canonicalArchiveCacheSHA(archiveSHAHex)
	if !ok {
		t.Fatalf("archive SHA %q is not cacheable", archiveSHAHex)
	}
	return archiveCache{dir: cacheDir}.pathForSHA(sha)
}
