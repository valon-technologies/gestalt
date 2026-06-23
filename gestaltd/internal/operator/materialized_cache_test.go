package operator

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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
	if _, err := cache.Put(context.Background(), req, source); err != nil {
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
	if _, err := cache.Put(context.Background(), req, source); err != nil {
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

func TestMaterializedCacheSeparatesExecutableArchiveByConfiguredName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := writeMaterializedCacheExecutableSource(t, dir, "source", "alpha")
	cache := materializedCache{dir: filepath.Join(dir, "cache")}
	req := materializedCacheExecutableRequest(dir, "alpha", "dest-alpha")
	if _, err := cache.Put(context.Background(), req, source); err != nil {
		t.Fatalf("Put: %v", err)
	}

	aliasReq := materializedCacheExecutableRequest(dir, "beta", "dest-beta")
	restore, err := cache.Restore(aliasReq)
	if err != nil {
		t.Fatalf("Restore alias archive: %v", err)
	}
	if restore == nil || restore.Result != materializedCacheMiss {
		t.Fatalf("alias Restore result = %#v, want miss", restore)
	}
}

func TestMaterializedCachePrefetchesRequestedRemoteEntriesBeforeRestore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	remote := newFakeMaterializedCacheRemote()
	source := writeMaterializedCacheUISource(t, dir, "source")
	req := materializedCacheUIRequest(dir, "alpha", "dest")
	unusedReq := materializedCacheUIRequest(dir, "unused", "dest-unused")
	unusedReq.ArchiveSHA256 = materializedCacheTestSHA256Hex([]byte("unused archive"))

	cold := materializedCache{dir: filepath.Join(dir, "cold"), remote: remote}
	if _, err := cold.Put(context.Background(), req, source); err != nil {
		t.Fatalf("Put requested entry: %v", err)
	}
	if _, err := cold.Put(context.Background(), unusedReq, source); err != nil {
		t.Fatalf("Put unrequested entry: %v", err)
	}
	if remote.puts != 2 {
		t.Fatalf("remote puts = %d, want 2", remote.puts)
	}
	key, eligible, err := materializedCacheKeyForRequest(req)
	if err != nil || !eligible {
		t.Fatalf("materializedCacheKeyForRequest eligible=%t err=%v", eligible, err)
	}
	unusedKey, eligible, err := materializedCacheKeyForRequest(unusedReq)
	if err != nil || !eligible {
		t.Fatalf("materializedCacheKeyForRequest unused eligible=%t err=%v", eligible, err)
	}

	warm := materializedCache{dir: filepath.Join(dir, "warm"), remote: remote}
	stats := warm.Prefetch(context.Background(), []materializedCacheRequest{req, req}, 2)
	if stats.Requests != 2 || stats.Eligible != 2 || len(stats.Keys) != 1 || stats.RemoteHits != 1 || stats.LocalHits != 0 || stats.RemoteMisses != 0 || stats.Failures != 0 || stats.Bytes == 0 {
		t.Fatalf("Prefetch stats = %+v, want one remote hit for duplicated request", stats)
	}
	if remote.gets != 1 || remote.getsByKey[key.Display] != 1 || remote.getsByKey[unusedKey.Display] != 0 {
		t.Fatalf("remote gets = %d by key = %#v, want only requested key fetched once", remote.gets, remote.getsByKey)
	}

	remote.getErr = errors.New("remote cache unavailable")
	stats = warm.Prefetch(context.Background(), []materializedCacheRequest{req}, 2)
	if stats.Requests != 1 || stats.Eligible != 1 || len(stats.Keys) != 1 || stats.LocalHits != 1 || stats.RemoteHits != 0 || stats.Failures != 0 || stats.Bytes != 0 {
		t.Fatalf("Prefetch local-hit stats = %+v, want one local hit without bytes", stats)
	}
	remote.getErr = nil

	restore, err := warm.Restore(req)
	if err != nil {
		t.Fatalf("Restore prefetched entry: %v", err)
	}
	if restore == nil || restore.Result != materializedCacheHit {
		t.Fatalf("prefetched Restore result = %#v, want hit", restore)
	}
	defer func() { _ = restore.cleanup() }()
	if err := restore.commit(); err != nil {
		t.Fatalf("commit prefetched restore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(req.DestinationDir, "manifest.yaml")); err != nil {
		t.Fatalf("restored manifest stat: %v", err)
	}
}

func TestMaterializedCachePrefetchRemoteMissIsNonFatal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	remote := newFakeMaterializedCacheRemote()
	req := materializedCacheUIRequest(dir, "alpha", "dest")

	cache := materializedCache{dir: filepath.Join(dir, "cache"), remote: remote}
	stats := cache.Prefetch(context.Background(), []materializedCacheRequest{req}, 1)
	if stats.Requests != 1 || stats.Eligible != 1 || len(stats.Keys) != 1 || stats.RemoteMisses != 1 || stats.Failures != 0 {
		t.Fatalf("Prefetch miss stats = %+v, want one remote miss", stats)
	}
	restore, err := cache.Restore(req)
	if err != nil {
		t.Fatalf("Restore after remote miss: %v", err)
	}
	if restore == nil || restore.Result != materializedCacheMiss {
		t.Fatalf("Restore remote miss = %#v, want miss", restore)
	}
}

func TestMaterializedCacheSkipsUnsafeRemoteArchiveDuringPrefetch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	remote := newFakeMaterializedCacheRemote()
	req := materializedCacheUIRequest(dir, "alpha", "dest")
	key, eligible, err := materializedCacheKeyForRequest(req)
	if err != nil || !eligible {
		t.Fatalf("materializedCacheKeyForRequest eligible=%t err=%v", eligible, err)
	}
	remote.objects[key.Display] = materializedCacheUnsafeArchive(t)

	cache := materializedCache{dir: filepath.Join(dir, "cache"), remote: remote}
	stats := cache.Prefetch(context.Background(), []materializedCacheRequest{req}, 1)
	if stats.Requests != 1 || stats.Eligible != 1 || len(stats.Keys) != 1 || stats.RemoteHits != 0 || stats.Failures != 1 {
		t.Fatalf("Prefetch unsafe stats = %+v, want one failure", stats)
	}
	restore, err := cache.Restore(req)
	if err != nil {
		t.Fatalf("Restore after unsafe prefetch: %v", err)
	}
	if restore == nil || restore.Result != materializedCacheMiss {
		t.Fatalf("Restore unsafe remote archive = %#v, want miss", restore)
	}
}

func TestMaterializedCachePutSkipsRemoteArchiveWhenObjectExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	remote := newFakeMaterializedCacheRemote()
	remote.exists = true
	source := writeMaterializedCacheUISource(t, dir, "source")
	req := materializedCacheUIRequest(dir, "alpha", "dest")
	cache := materializedCache{dir: filepath.Join(dir, "cache"), remote: remote}

	result, err := cache.Put(context.Background(), req, source)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if remote.existsCalls != 1 {
		t.Fatalf("remote exists calls = %d, want 1", remote.existsCalls)
	}
	if remote.puts != 0 {
		t.Fatalf("remote puts = %d, want 0", remote.puts)
	}
	if !result.Timings.RemoteSkippedExisting {
		t.Fatal("RemoteSkippedExisting = false, want true")
	}
	if result.Timings.RemoteArchive != 0 || result.Timings.RemoteUpload != 0 {
		t.Fatalf("remote archive/upload timings = %s/%s, want zero", result.Timings.RemoteArchive, result.Timings.RemoteUpload)
	}
	assertNoMaterializedCacheArchiveTemps(t, filepath.Join(dir, "cache"), result.Key)
}

func TestMaterializedCachePutRemoteExistsErrorSkipsArchive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	remote := newFakeMaterializedCacheRemote()
	remote.existsErr = errors.New("metadata unavailable")
	source := writeMaterializedCacheUISource(t, dir, "source")
	req := materializedCacheUIRequest(dir, "alpha", "dest")
	cache := materializedCache{dir: filepath.Join(dir, "cache"), remote: remote}

	result, err := cache.Put(context.Background(), req, source)
	if err == nil {
		t.Fatal("Put error = nil, want remote exists error")
	}
	if remote.existsCalls != 1 {
		t.Fatalf("remote exists calls = %d, want 1", remote.existsCalls)
	}
	if remote.puts != 0 {
		t.Fatalf("remote puts = %d, want 0", remote.puts)
	}
	if result.Files == 0 || result.Bytes == 0 {
		t.Fatalf("put result files/bytes = %d/%d, want populated result", result.Files, result.Bytes)
	}
	if result.Timings.RemoteArchive != 0 || result.Timings.RemoteUpload != 0 {
		t.Fatalf("remote archive/upload timings = %s/%s, want zero", result.Timings.RemoteArchive, result.Timings.RemoteUpload)
	}
	assertNoMaterializedCacheArchiveTemps(t, filepath.Join(dir, "cache"), result.Key)
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

func writeMaterializedCacheExecutableSource(t *testing.T, dir, name, executableName string) string {
	t.Helper()
	source := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(source, "bin"), 0o755); err != nil {
		t.Fatalf("create executable source bin: %v", err)
	}
	executable := filepath.Join(source, "bin", executableName)
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	manifest := []byte("kind: agent\nsource: github.com/acme/pkg/agent\nversion: 0.0.1\ndisplayName: Test Agent\ndescription: Test Agent\nentrypoint:\n  artifactPath: bin/" + executableName + "\n")
	if err := os.WriteFile(filepath.Join(source, "manifest.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("write executable manifest: %v", err)
	}
	return source
}

func materializedCacheExecutableRequest(dir, name, destName string) materializedCacheRequest {
	req := materializedCacheUIRequest(dir, name, destName)
	req.Subject = `agent "` + name + `"`
	req.Kind = "agent"
	req.Package = "github.com/acme/pkg/agent"
	return req
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

type fakeMaterializedCacheRemote struct {
	objects     map[string][]byte
	gets        int
	getsByKey   map[string]int
	existsCalls int
	puts        int
	exists      bool
	getErr      error
	existsErr   error
}

func newFakeMaterializedCacheRemote() *fakeMaterializedCacheRemote {
	return &fakeMaterializedCacheRemote{
		objects:   map[string][]byte{},
		getsByKey: map[string]int{},
	}
}

func (r *fakeMaterializedCacheRemote) Get(_ context.Context, key materializedCacheKey) (io.ReadCloser, error) {
	r.gets++
	r.getsByKey[key.Display]++
	if r.getErr != nil {
		return nil, r.getErr
	}
	data := r.objects[key.Display]
	if data == nil {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (r *fakeMaterializedCacheRemote) Exists(_ context.Context, _ materializedCacheKey) (bool, error) {
	r.existsCalls++
	if r.existsErr != nil {
		return false, r.existsErr
	}
	return r.exists, nil
}

func (r *fakeMaterializedCacheRemote) Put(_ context.Context, key materializedCacheKey, archivePath string) error {
	r.puts++
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return err
	}
	r.objects[key.Display] = data
	return nil
}

func materializedCacheUnsafeArchive(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     materializedCacheRootDir + "/link",
		Typeflag: tar.TypeSymlink,
		Linkname: materializedCacheEntryFile,
		Mode:     0o777,
	}); err != nil {
		t.Fatalf("write unsafe archive header: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close unsafe archive tar: %v", err)
	}
	return buf.Bytes()
}

func assertNoMaterializedCacheArchiveTemps(t *testing.T, cacheDir string, key materializedCacheKey) {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(filepath.Dir((materializedCache{dir: cacheDir}).entryDir(key)), ".*.archive-*.tar"))
	if err != nil {
		t.Fatalf("glob archive temp files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("archive temp files = %v, want none", matches)
	}
}
