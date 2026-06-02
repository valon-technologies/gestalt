package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestArchiveCacheGetPutRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := []byte("archive bytes")
	sha := testSHA256Hex(data)
	sourcePath := filepath.Join(dir, "source.tar.gz")
	if err := os.WriteFile(sourcePath, data, 0o644); err != nil {
		t.Fatalf("write source archive: %v", err)
	}

	cache := archiveCache{dir: filepath.Join(dir, "cache")}
	if err := cache.Put(sourcePath, sha); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := cache.Put(sourcePath, strings.ToUpper(sha)); err != nil {
		t.Fatalf("second Put with uppercase digest: %v", err)
	}
	got, result, err := cache.Get(sha)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if result != archiveCacheHit {
		t.Fatalf("Get result = %v, want %v", result, archiveCacheHit)
	}
	if got.LocalPath == cache.pathForSHA(sha) {
		t.Fatalf("LocalPath = cache path %q, want private temp copy", got.LocalPath)
	}
	if got.SHA256Hex != sha {
		t.Fatalf("SHA256Hex = %q, want %q", got.SHA256Hex, sha)
	}
	if gotData, err := os.ReadFile(got.LocalPath); err != nil || string(gotData) != string(data) {
		t.Fatalf("read temp cache hit data = %q, %v; want %q", gotData, err, data)
	}
	got.Cleanup()
	if _, err := os.Stat(got.LocalPath); !os.IsNotExist(err) {
		t.Fatalf("temp cache hit after cleanup stat err = %v, want not exist", err)
	}

	got, result, err = cache.Get(strings.ToUpper(sha))
	if err != nil {
		t.Fatalf("Get uppercase: %v", err)
	}
	if result != archiveCacheHit {
		t.Fatalf("Get uppercase result = %v, want %v", result, archiveCacheHit)
	}
	if got.SHA256Hex != sha {
		t.Fatalf("uppercase SHA256Hex = %q, want %q", got.SHA256Hex, sha)
	}
	got.Cleanup()
}

func TestArchiveCacheRejectsInvalidDigest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := []byte("archive bytes")
	sha := testSHA256Hex(data)
	sourcePath := filepath.Join(dir, "source.tar.gz")
	if err := os.WriteFile(sourcePath, data, 0o644); err != nil {
		t.Fatalf("write source archive: %v", err)
	}
	cache := archiveCache{dir: filepath.Join(dir, "cache")}

	for _, digest := range []string{"", sha[:63], strings.Repeat("g", 64)} {
		if _, result, err := cache.Get(digest); err == nil || result != archiveCacheRejected {
			t.Fatalf("Get(%q) error = nil, want invalid digest error", digest)
		}
		if err := cache.Put(sourcePath, digest); err == nil {
			t.Fatalf("Put(%q) error = nil, want invalid digest error", digest)
		}
	}
}

func TestArchiveCacheInvalidatesCorruptAndOversizedEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := archiveCache{dir: filepath.Join(dir, "cache")}
	sha := testSHA256Hex([]byte("expected archive"))

	corruptPath := cache.pathForSHA(sha)
	if err := os.MkdirAll(filepath.Dir(corruptPath), 0o755); err != nil {
		t.Fatalf("create corrupt parent: %v", err)
	}
	if err := os.WriteFile(corruptPath, []byte("wrong archive"), 0o644); err != nil {
		t.Fatalf("write corrupt cache entry: %v", err)
	}
	if _, result, err := cache.Get(sha); err != nil || result != archiveCacheInvalid {
		t.Fatalf("Get corrupt entry result=%v err=%v, want invalid without error", result, err)
	}
	if _, err := os.Stat(corruptPath); !os.IsNotExist(err) {
		t.Fatalf("corrupt cache entry still exists, stat err = %v", err)
	}

	oversizedSHA := testSHA256Hex([]byte("oversized archive"))
	oversizedPath := cache.pathForSHA(oversizedSHA)
	if err := os.MkdirAll(filepath.Dir(oversizedPath), 0o755); err != nil {
		t.Fatalf("create oversized parent: %v", err)
	}
	f, err := os.Create(oversizedPath)
	if err != nil {
		t.Fatalf("create oversized cache entry: %v", err)
	}
	if err := f.Truncate(int64(providerpkg.MaxPackageBytes) + 1); err != nil {
		_ = f.Close()
		t.Fatalf("truncate oversized cache entry: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close oversized cache entry: %v", err)
	}
	if _, result, err := cache.Get(oversizedSHA); err != nil || result != archiveCacheInvalid {
		t.Fatalf("Get oversized entry result=%v err=%v, want invalid without error", result, err)
	}
	if _, err := os.Stat(oversizedPath); !os.IsNotExist(err) {
		t.Fatalf("oversized cache entry still exists, stat err = %v", err)
	}
}

func TestArchiveCacheRejectsSymlinkAndNonRegularEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := archiveCache{dir: filepath.Join(dir, "cache")}
	sha := testSHA256Hex([]byte("expected archive"))
	targetPath := cache.pathForSHA(sha)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("create target parent: %v", err)
	}

	linkTarget := filepath.Join(dir, "link-target.tar.gz")
	if err := os.WriteFile(linkTarget, []byte("expected archive"), 0o644); err != nil {
		t.Fatalf("write link target: %v", err)
	}
	if err := os.Symlink(linkTarget, targetPath); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, result, err := cache.Get(sha); err == nil || result != archiveCacheRejected || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Get symlink result=%v error=%v, want rejected symlink error", result, err)
	}
	if err := os.Remove(targetPath); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	if err := os.Mkdir(targetPath, 0o755); err != nil {
		t.Fatalf("create directory cache entry: %v", err)
	}
	if _, result, err := cache.Get(sha); err == nil || result != archiveCacheRejected || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("Get directory result=%v error=%v, want rejected non-regular error", result, err)
	}
}

func TestArchiveCachePutReplacesInvalidTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := archiveCache{dir: filepath.Join(dir, "cache")}
	data := []byte("archive bytes")
	sha := testSHA256Hex(data)
	sourcePath := filepath.Join(dir, "source.tar.gz")
	if err := os.WriteFile(sourcePath, data, 0o644); err != nil {
		t.Fatalf("write source archive: %v", err)
	}
	targetPath := cache.pathForSHA(sha)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("create target parent: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("wrong archive"), 0o644); err != nil {
		t.Fatalf("write invalid target: %v", err)
	}
	if err := cache.Put(sourcePath, sha); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("target data = %q, want %q", got, data)
	}
}

func TestDownloadArchiveWithCacheFallsBackWhenStoreFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := []byte("archive bytes")
	sha := testSHA256Hex(data)
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	cache := archiveCache{dir: filepath.Join(dir, "cache")}
	shardDir := filepath.Dir(cache.pathForSHA(sha))
	if err := os.MkdirAll(shardDir, 0o755); err != nil {
		t.Fatalf("create shard dir: %v", err)
	}
	if err := os.Chmod(shardDir, 0o555); err != nil {
		t.Fatalf("chmod shard dir readonly: %v", err)
	}
	defer func() { _ = os.Chmod(shardDir, 0o755) }()

	download, err := downloadArchiveForSourceWithCache(context.Background(), srv.Client(), "", srv.URL, sha, cache.dir, nil)
	if err != nil {
		t.Fatalf("downloadArchiveForSourceWithCache: %v", err)
	}
	defer download.Cleanup()
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
	if download.SHA256Hex != sha {
		t.Fatalf("SHA256Hex = %q, want %q", download.SHA256Hex, sha)
	}
	if _, err := os.Stat(download.LocalPath); err != nil {
		t.Fatalf("download local path stat: %v", err)
	}
	if _, err := os.Stat(cache.pathForSHA(sha)); !os.IsNotExist(err) {
		t.Fatalf("cache archive stat err = %v, want not exist", err)
	}
}

func TestDownloadArchiveWithCacheVerifiesExpectedSHA(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := []byte("archive bytes")
	sha := testSHA256Hex(data)
	wrongSHA := testSHA256Hex([]byte("wrong archive bytes"))
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	cache := archiveCache{dir: filepath.Join(dir, "cache")}
	download, err := downloadArchiveForSourceWithCache(context.Background(), srv.Client(), "", srv.URL, wrongSHA, cache.dir, nil)
	if err == nil {
		if download != nil {
			download.Cleanup()
		}
		t.Fatal("downloadArchiveForSourceWithCache mismatch error = nil")
	}
	var mismatch archiveDigestMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("downloadArchiveForSourceWithCache mismatch error = %v, want archiveDigestMismatchError", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
	if _, err := os.Stat(cache.pathForSHA(sha)); !os.IsNotExist(err) {
		t.Fatalf("matching cache archive stat err = %v, want not exist", err)
	}

	_, err = downloadArchiveForSourceWithCache(context.Background(), srv.Client(), "", srv.URL, "abc123", cache.dir, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid archive sha256") {
		t.Fatalf("downloadArchiveForSourceWithCache invalid digest error = %v, want invalid archive sha256", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count after invalid digest = %d, want 1", got)
	}
}

func TestDownloadArchiveWithCacheRecordsMetrics(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := []byte("archive bytes")
	sha := testSHA256Hex(data)
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	cacheDir := filepath.Join(dir, "cache")
	coldMetrics := NewSyncMetricsRecorder()
	coldMetrics.SetPaths("", "", cacheDir, true, true)
	download, err := downloadArchiveForSourceWithCache(context.Background(), srv.Client(), "", srv.URL, sha, cacheDir, newSyncArchiveDownloadObserver(coldMetrics, "provider alpha"))
	if err != nil {
		t.Fatalf("cold downloadArchiveForSourceWithCache: %v", err)
	}
	download.Cleanup()
	cold := coldMetrics.Snapshot()
	if cold.Archives.Requests != 1 || cold.Archives.Cache.Eligible != 1 || cold.Archives.Cache.Misses != 1 || cold.Archives.Downloads.Count != 1 || cold.Archives.Cache.Puts != 1 {
		t.Fatalf("cold metrics = %+v, want one eligible miss, download, and put", cold.Archives)
	}
	if len(cold.Archives.Fetches) != 1 {
		t.Fatalf("cold fetches len = %d, want 1", len(cold.Archives.Fetches))
	}
	if got := cold.Archives.Fetches[0]; got.Subject != "provider alpha" || got.CacheResult != syncArchiveCacheResultMiss || !got.Downloaded || got.Bytes != int64(len(data)) {
		t.Fatalf("cold fetch = %+v, want miss/downloaded/%d bytes", got, len(data))
	}

	warmMetrics := NewSyncMetricsRecorder()
	warmMetrics.SetPaths("", "", cacheDir, true, true)
	download, err = downloadArchiveForSourceWithCache(context.Background(), srv.Client(), "", srv.URL, sha, cacheDir, newSyncArchiveDownloadObserver(warmMetrics, "provider alpha"))
	if err != nil {
		t.Fatalf("warm downloadArchiveForSourceWithCache: %v", err)
	}
	download.Cleanup()
	warm := warmMetrics.Snapshot()
	if warm.Archives.Requests != 1 || warm.Archives.Cache.Eligible != 1 || warm.Archives.Cache.Hits != 1 || warm.Archives.Downloads.Count != 0 {
		t.Fatalf("warm metrics = %+v, want one eligible hit and no download", warm.Archives)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want only cold download to hit server", got)
	}
}

func TestDownloadArchiveMetricsClassifiesDisabledAndUncacheable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := []byte("archive bytes")
	sha := testSHA256Hex(data)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	disabledMetrics := NewSyncMetricsRecorder()
	download, err := downloadArchiveForSourceWithCache(context.Background(), srv.Client(), "", srv.URL, sha, "", newSyncArchiveDownloadObserver(disabledMetrics, "provider alpha"))
	if err != nil {
		t.Fatalf("disabled cache downloadArchiveForSourceWithCache: %v", err)
	}
	download.Cleanup()
	disabled := disabledMetrics.Snapshot()
	if disabled.Archives.Cache.Eligible != 1 || disabled.Archives.Cache.Disabled != 1 || disabled.Archives.Cache.Uncacheable != 0 || disabled.Archives.Downloads.Count != 1 {
		t.Fatalf("disabled cache metrics = %+v, want eligible disabled remote download", disabled.Archives)
	}

	localPath := filepath.Join(dir, "local.tar.gz")
	if err := os.WriteFile(localPath, data, 0o644); err != nil {
		t.Fatalf("write local archive: %v", err)
	}
	localMetrics := NewSyncMetricsRecorder()
	download, err = downloadArchiveForSourceWithCache(context.Background(), nil, "", localPath, sha, filepath.Join(dir, "cache"), newSyncArchiveDownloadObserver(localMetrics, "provider alpha"))
	if err != nil {
		t.Fatalf("local downloadArchiveForSourceWithCache: %v", err)
	}
	download.Cleanup()
	local := localMetrics.Snapshot()
	if local.Archives.Cache.Eligible != 0 || local.Archives.Cache.Uncacheable != 1 || local.Archives.Downloads.Count != 0 {
		t.Fatalf("local metrics = %+v, want uncacheable local copy with no remote download", local.Archives)
	}
}

func TestLockfileNormalizesArchiveSHA256(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, LockfileName)
	sha := testSHA256Hex([]byte("archive bytes"))
	lock := &Lockfile{
		Providers: map[string]LockEntry{
			"alpha": {
				Package: "github.com/acme/tools/alpha",
				Kind:    "app",
				Runtime: providerReleaseRuntimeExecutable,
				Archives: map[string]LockArchive{
					"generic": {URL: "https://example.com/alpha.tar.gz", SHA256: strings.ToUpper(sha)},
				},
			},
		},
	}
	if err := WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	written, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := written.Providers["alpha"].Archives["generic"].SHA256; got != sha {
		t.Fatalf("read archive SHA256 = %q, want %q", got, sha)
	}
}

func testSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
