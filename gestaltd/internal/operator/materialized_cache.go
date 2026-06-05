package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	materializedCacheSchema        = "gestaltd-materialized-cache-entry"
	materializedCacheSchemaVersion = 1
	materializedCacheBucketVersion = "materialized/v1"
	materializedCacheRootDir       = "root"
	materializedCacheEntryFile     = "entry.json"
	materializedCacheVersion       = "package-install-v1"
)

type materializedCache struct {
	dir    string
	remote materializedCacheRemote
}

type materializedCacheLookupResult string

const (
	materializedCacheMiss    materializedCacheLookupResult = "miss"
	materializedCacheHit     materializedCacheLookupResult = "hit"
	materializedCacheInvalid materializedCacheLookupResult = "invalid"
)

type materializedCacheRequest struct {
	Subject        string
	Kind           string
	Name           string
	SourceKind     string
	ArchiveSHA256  string
	ResolvedKey    string
	Platform       string
	Package        string
	Version        string
	DestinationDir string
}

type materializedCacheKey struct {
	Path                string
	Display             string
	ArchiveSHA256       string
	ResolvedKeyHash     string
	Platform            string
	MaterializerVersion string
}

type materializedCacheEntry struct {
	Schema              string                  `json:"schema"`
	SchemaVersion       int                     `json:"schemaVersion"`
	MaterializerVersion string                  `json:"materializerVersion"`
	ArchiveSHA256       string                  `json:"archiveSHA256"`
	Platform            string                  `json:"platform"`
	ResolvedKeyHash     string                  `json:"resolvedKeyHash"`
	Subject             string                  `json:"subject,omitempty"`
	Kind                string                  `json:"kind,omitempty"`
	Name                string                  `json:"name,omitempty"`
	SourceKind          string                  `json:"sourceKind,omitempty"`
	Package             string                  `json:"package,omitempty"`
	Version             string                  `json:"version,omitempty"`
	OutputDigest        string                  `json:"outputDigest"`
	Files               []materializedCacheFile `json:"files"`
}

type materializedCacheFile struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
}

type materializedCacheRestore struct {
	Install *preparedInstall
	Result  materializedCacheLookupResult
	Key     materializedCacheKey
	Bytes   int64
	Files   int

	cleanup func() error
	commit  func() error
}

type materializedCachePutResult struct {
	Key     materializedCacheKey
	Files   int
	Bytes   int64
	Timings materializedCachePutTimings
}

type materializedCachePutTimings struct {
	LocalInspect          time.Duration
	LocalWrite            time.Duration
	RemoteExists          time.Duration
	RemoteArchive         time.Duration
	RemoteUpload          time.Duration
	RemoteSkippedExisting bool
}

func materializedCacheKeyForRequest(req materializedCacheRequest) (materializedCacheKey, bool, error) {
	sha, hasSHA, err := normalizeArchiveSHA256(req.ArchiveSHA256)
	if err != nil || !hasSHA {
		return materializedCacheKey{}, false, err
	}
	if strings.TrimSpace(req.Platform) == "" || strings.TrimSpace(req.ResolvedKey) == "" {
		return materializedCacheKey{}, false, nil
	}
	resolvedHash := sha256Hex([]byte(req.ResolvedKey))
	platformPath := strings.ReplaceAll(req.Platform, "/", "_")
	return materializedCacheKeyForParts(req.Platform, platformPath, sha, resolvedHash), true, nil
}

func materializedCacheKeyFromDisplay(display string) (materializedCacheKey, bool) {
	display = strings.TrimSpace(filepath.ToSlash(display))
	parts := strings.Split(display, "/")
	if len(parts) != 7 || parts[0]+"/"+parts[1] != materializedCacheBucketVersion || parts[3] != "sha256" {
		return materializedCacheKey{}, false
	}
	platformPath := parts[2]
	sha, ok := canonicalArchiveSHA256(parts[5])
	if !ok || parts[4] != sha[:2] {
		return materializedCacheKey{}, false
	}
	resolvedHash, ok := canonicalArchiveSHA256(parts[6])
	if !ok {
		return materializedCacheKey{}, false
	}
	key := materializedCacheKeyForParts(strings.ReplaceAll(platformPath, "_", "/"), platformPath, sha, resolvedHash)
	if clean, err := validateMaterializedCachePath(filepath.ToSlash(key.Path)); err != nil || clean != filepath.ToSlash(key.Path) || key.Display != strings.Join(parts, "/") {
		return materializedCacheKey{}, false
	}
	return key, true
}

func materializedCacheKeyForParts(platform, platformPath, sha, resolvedHash string) materializedCacheKey {
	return materializedCacheKey{
		Path:                filepath.Join(platformPath, "sha256", sha[:2], sha, resolvedHash),
		Display:             strings.Join([]string{materializedCacheBucketVersion, platformPath, "sha256", sha[:2], sha, resolvedHash}, "/"),
		ArchiveSHA256:       sha,
		ResolvedKeyHash:     resolvedHash,
		Platform:            platform,
		MaterializerVersion: materializedCacheVersion,
	}
}

func (c materializedCache) Restore(req materializedCacheRequest) (*materializedCacheRestore, error) {
	key, eligible, err := materializedCacheKeyForRequest(req)
	if err != nil || !eligible {
		return nil, err
	}
	entryDir := c.entryDir(key)
	fallback := materializedCacheMiss
	entry, err := c.readEntryForKey(entryDir, key)
	if err == nil {
		restore, err := c.stageRestore(entryDir, req.DestinationDir, entry)
		if err == nil {
			restore.Result = materializedCacheHit
			restore.Key = key
			return restore, nil
		}
		fallback = materializedCacheInvalid
	} else if !os.IsNotExist(err) {
		fallback = materializedCacheInvalid
	}
	return materializedCacheRestoreResult(key, fallback)
}

func materializedCacheRestoreResult(key materializedCacheKey, result materializedCacheLookupResult) (*materializedCacheRestore, error) {
	return &materializedCacheRestore{Result: result, Key: key}, nil
}

func (c materializedCache) Put(ctx context.Context, req materializedCacheRequest, sourceDir string) (materializedCachePutResult, error) {
	key, eligible, err := materializedCacheKeyForRequest(req)
	result := materializedCachePutResult{Key: key}
	if err != nil {
		return result, err
	}
	if !eligible {
		return result, nil
	}
	inspectStart := time.Now()
	files, bytes, digest, err := inspectMaterializedCacheFiles(sourceDir)
	result.Timings.LocalInspect = time.Since(inspectStart)
	result.Files = len(files)
	result.Bytes = bytes
	if err != nil {
		return result, err
	}
	entry := materializedCacheEntry{
		Schema:              materializedCacheSchema,
		SchemaVersion:       materializedCacheSchemaVersion,
		MaterializerVersion: materializedCacheVersion,
		ArchiveSHA256:       key.ArchiveSHA256,
		Platform:            key.Platform,
		ResolvedKeyHash:     key.ResolvedKeyHash,
		Subject:             req.Subject,
		Kind:                req.Kind,
		Name:                req.Name,
		SourceKind:          req.SourceKind,
		Package:             req.Package,
		Version:             req.Version,
		OutputDigest:        digest,
		Files:               files,
	}

	writeStart := time.Now()
	entryDir := c.entryDir(key)
	parentDir := filepath.Dir(entryDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		result.Timings.LocalWrite = time.Since(writeStart)
		return result, fmt.Errorf("create materialized cache parent: %w", err)
	}
	tmpDir, err := os.MkdirTemp(parentDir, "."+filepath.Base(entryDir)+".tmp-*")
	if err != nil {
		result.Timings.LocalWrite = time.Since(writeStart)
		return result, fmt.Errorf("create materialized cache temp entry: %w", err)
	}
	keepTmp := false
	defer func() {
		if !keepTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()
	rootDir := filepath.Join(tmpDir, materializedCacheRootDir)
	if err := copyMaterializedFiles(sourceDir, rootDir, files); err != nil {
		result.Timings.LocalWrite = time.Since(writeStart)
		return result, err
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		result.Timings.LocalWrite = time.Since(writeStart)
		return result, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, materializedCacheEntryFile), append(data, '\n'), 0o644); err != nil {
		result.Timings.LocalWrite = time.Since(writeStart)
		return result, fmt.Errorf("write materialized cache entry metadata: %w", err)
	}
	if err := os.RemoveAll(entryDir); err != nil {
		result.Timings.LocalWrite = time.Since(writeStart)
		return result, fmt.Errorf("remove stale materialized cache entry: %w", err)
	}
	if err := os.Rename(tmpDir, entryDir); err != nil {
		result.Timings.LocalWrite = time.Since(writeStart)
		return result, fmt.Errorf("commit materialized cache entry: %w", err)
	}
	keepTmp = true
	result.Timings.LocalWrite = time.Since(writeStart)
	if c.remote != nil {
		existsStart := time.Now()
		exists, err := c.remote.Exists(ctx, key)
		result.Timings.RemoteExists = time.Since(existsStart)
		if err != nil {
			return result, fmt.Errorf("check materialized cache remote object: %w", err)
		}
		if exists {
			result.Timings.RemoteSkippedExisting = true
			return result, nil
		}
		archivePath, err := os.CreateTemp(parentDir, "."+filepath.Base(entryDir)+".archive-*.tar")
		if err != nil {
			return result, fmt.Errorf("create materialized cache archive temp file: %w", err)
		}
		archiveName := archivePath.Name()
		if err := archivePath.Close(); err != nil {
			_ = os.Remove(archiveName)
			return result, fmt.Errorf("close materialized cache archive temp file: %w", err)
		}
		defer func() { _ = os.Remove(archiveName) }()
		archiveStart := time.Now()
		if err := writeMaterializedCacheEntryArchive(entryDir, archiveName); err != nil {
			result.Timings.RemoteArchive = time.Since(archiveStart)
			return result, err
		}
		result.Timings.RemoteArchive = time.Since(archiveStart)
		uploadStart := time.Now()
		if err := c.remote.Put(ctx, key, archiveName); err != nil {
			result.Timings.RemoteUpload = time.Since(uploadStart)
			return result, err
		}
		result.Timings.RemoteUpload = time.Since(uploadStart)
	}
	return result, nil
}

func (c materializedCache) entryDir(key materializedCacheKey) string {
	return filepath.Join(c.dir, materializedCacheBucketVersion, key.Path)
}

func (c materializedCache) readEntryForKey(entryDir string, key materializedCacheKey) (*materializedCacheEntry, error) {
	data, err := os.ReadFile(filepath.Join(entryDir, materializedCacheEntryFile))
	if err != nil {
		return nil, err
	}
	var entry materializedCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("decode materialized cache entry: %w", err)
	}
	if entry.Schema != materializedCacheSchema ||
		entry.SchemaVersion != materializedCacheSchemaVersion ||
		entry.MaterializerVersion != materializedCacheVersion ||
		entry.ArchiveSHA256 != key.ArchiveSHA256 ||
		entry.Platform != key.Platform ||
		entry.ResolvedKeyHash != key.ResolvedKeyHash {
		return nil, fmt.Errorf("materialized cache entry metadata mismatch")
	}
	_, digest, err := materializedCacheOutputDigest(entry.Files)
	if err != nil {
		return nil, err
	}
	if entry.OutputDigest != digest {
		return nil, fmt.Errorf("materialized cache entry output digest mismatch")
	}
	return &entry, nil
}

func (c materializedCache) stageRestore(entryDir, destDir string, entry *materializedCacheEntry) (*materializedCacheRestore, error) {
	parentDir := filepath.Dir(destDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return nil, fmt.Errorf("create destination parent directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(parentDir, filepath.Base(destDir)+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create temp materialized cache restore: %w", err)
	}
	cleanupDir := tempDir
	cleanup := func() error {
		if cleanupDir == "" {
			return nil
		}
		return os.RemoveAll(cleanupDir)
	}
	if err := copyMaterializedFiles(filepath.Join(entryDir, materializedCacheRootDir), tempDir, entry.Files); err != nil {
		_ = cleanup()
		return nil, err
	}
	install, err := inspectPreparedInstall(tempDir)
	if err != nil {
		_ = cleanup()
		return nil, fmt.Errorf("inspect restored materialized cache entry: %w", err)
	}
	bytes, _, _ := materializedCacheOutputDigest(entry.Files)
	restore := &materializedCacheRestore{
		Install: install,
		Bytes:   bytes,
		Files:   len(entry.Files),
		cleanup: cleanup,
	}
	restore.commit = func() error {
		if err := activatePreparedInstallDir(destDir, tempDir); err != nil {
			return err
		}
		cleanupDir = ""
		return nil
	}
	return restore, nil
}

func inspectMaterializedCacheFiles(root string) ([]materializedCacheFile, int64, string, error) {
	var files []materializedCacheFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("materialized cache does not support symlink %s", rel)
		}
		if d.IsDir() {
			files = append(files, materializedCacheFile{
				Path: rel,
				Type: "dir",
				Mode: uint32(info.Mode().Perm()),
			})
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("materialized cache does not support non-regular file %s", rel)
		}
		digest, err := fileSHA256Hex(path)
		if err != nil {
			return err
		}
		files = append(files, materializedCacheFile{
			Path:   rel,
			Type:   "file",
			SHA256: digest,
			Size:   info.Size(),
			Mode:   uint32(info.Mode().Perm()),
		})
		return nil
	})
	if err != nil {
		return nil, 0, "", err
	}
	slices.SortFunc(files, func(a, b materializedCacheFile) int {
		return strings.Compare(a.Path, b.Path)
	})
	bytes, digest, err := materializedCacheOutputDigest(files)
	if err != nil {
		return nil, 0, "", err
	}
	return files, bytes, digest, nil
}

func materializedCacheOutputDigest(files []materializedCacheFile) (int64, string, error) {
	seen := make(map[string]struct{}, len(files))
	var bytes int64
	h := sha256.New()
	for _, file := range files {
		path, err := validateMaterializedCachePath(file.Path)
		if err != nil {
			return 0, "", err
		}
		if _, ok := seen[path]; ok {
			return 0, "", fmt.Errorf("duplicate materialized cache path %s", path)
		}
		seen[path] = struct{}{}
		switch file.Type {
		case "dir":
			if file.SHA256 != "" || file.Size != 0 {
				return 0, "", fmt.Errorf("invalid materialized cache directory metadata for %s", path)
			}
			_, _ = fmt.Fprintf(h, "dir\x00%s\x00%o\x00", path, file.Mode)
		case "file":
			sha, ok := canonicalArchiveSHA256(file.SHA256)
			if !ok {
				return 0, "", fmt.Errorf("invalid materialized cache file sha256 for %s", path)
			}
			if file.Size < 0 {
				return 0, "", fmt.Errorf("invalid materialized cache file size for %s", path)
			}
			bytes += file.Size
			_, _ = fmt.Fprintf(h, "file\x00%s\x00%d\x00%o\x00%s\x00", path, file.Size, file.Mode, sha)
		default:
			return 0, "", fmt.Errorf("invalid materialized cache entry type for %s", path)
		}
	}
	return bytes, hex.EncodeToString(h.Sum(nil)), nil
}

func copyMaterializedFiles(sourceRoot, destRoot string, files []materializedCacheFile) error {
	for _, file := range files {
		rel, err := validateMaterializedCachePath(file.Path)
		if err != nil {
			return err
		}
		source := filepath.Join(sourceRoot, filepath.FromSlash(rel))
		dest := filepath.Join(destRoot, filepath.FromSlash(rel))
		if file.Type == "dir" {
			if err := os.MkdirAll(dest, os.FileMode(file.Mode)&0o777); err != nil {
				return fmt.Errorf("create directory %s: %w", rel, err)
			}
			if err := os.Chmod(dest, os.FileMode(file.Mode)&0o777); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create parent for %s: %w", rel, err)
		}
		if err := copyMaterializedFile(source, dest, file); err != nil {
			return err
		}
	}
	return nil
}

func validateMaterializedCacheEntryFiles(entryDir string, entry *materializedCacheEntry) error {
	for _, file := range entry.Files {
		rel, err := validateMaterializedCachePath(file.Path)
		if err != nil {
			return err
		}
		source := filepath.Join(entryDir, materializedCacheRootDir, filepath.FromSlash(rel))
		if file.Type == "dir" {
			info, err := os.Lstat(source)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("materialized cache directory %s is invalid", file.Path)
			}
			continue
		}
		if err := validateMaterializedFile(source, file); err != nil {
			return err
		}
	}
	return nil
}

func copyMaterializedFile(source, dest string, file materializedCacheFile) error {
	want, err := validateMaterializedFileMetadata(source, file)
	if err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(file.Mode)&0o777)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("materialized cache file %s digest mismatch", file.Path)
	}
	if err := os.Chmod(dest, os.FileMode(file.Mode)&0o777); err != nil {
		return err
	}
	return nil
}

func validateMaterializedFile(source string, file materializedCacheFile) error {
	want, err := validateMaterializedFileMetadata(source, file)
	if err != nil {
		return err
	}
	got, err := fileSHA256Hex(source)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("materialized cache file %s digest mismatch", file.Path)
	}
	return nil
}

func validateMaterializedFileMetadata(source string, file materializedCacheFile) (string, error) {
	if file.Type != "file" {
		return "", fmt.Errorf("materialized cache entry %s is %q, want file", file.Path, file.Type)
	}
	info, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("materialized cache file %s is a symlink", file.Path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("materialized cache file %s is not regular", file.Path)
	}
	if info.Size() != file.Size {
		return "", fmt.Errorf("materialized cache file %s size mismatch", file.Path)
	}
	want, ok := canonicalArchiveSHA256(file.SHA256)
	if !ok {
		return "", fmt.Errorf("invalid materialized cache file sha256 for %s", file.Path)
	}
	return want, nil
}

func validateMaterializedCachePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return "", fmt.Errorf("invalid materialized cache path %q", path)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("invalid materialized cache path %q", path)
	}
	return clean, nil
}

func fileSHA256Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
