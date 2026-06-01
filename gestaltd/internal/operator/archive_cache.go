package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

type archiveCache struct {
	dir string
}

type archiveDigestMismatchError struct {
	actual   string
	expected string
}

func (e archiveDigestMismatchError) Error() string {
	return fmt.Sprintf("archive digest mismatch: got %s, want %s", e.actual, e.expected)
}

type archiveSizeLimitError struct {
	subject  string
	maxBytes int64
}

func (e archiveSizeLimitError) Error() string {
	return fmt.Sprintf("%s exceeds %d byte limit", e.subject, e.maxBytes)
}

type archiveCacheLookupResult int

const (
	archiveCacheMiss archiveCacheLookupResult = iota
	archiveCacheHit
	archiveCacheInvalid
	archiveCacheRejected
)

func downloadArchiveForSourceWithCache(ctx context.Context, client *http.Client, token, archiveURL, expectedSHA, cacheDir string) (*providerpkg.DownloadResult, error) {
	sha, hasExpectedSHA, err := normalizeArchiveSHA256(expectedSHA)
	if err != nil {
		return nil, err
	}

	useCache := cacheDir != "" && hasExpectedSHA && isRemoteReleaseMetadataLocation(archiveURL)
	if useCache {
		cache := archiveCache{dir: cacheDir}
		cached, result, err := cache.Get(sha)
		if err != nil {
			return nil, fmt.Errorf("read archive cache: %w", err)
		}
		switch result {
		case archiveCacheHit:
			return cached, nil
		case archiveCacheMiss, archiveCacheInvalid:
		case archiveCacheRejected:
			return nil, fmt.Errorf("read archive cache: rejected cached archive")
		}
	}

	download, err := downloadArchiveForSource(ctx, client, token, archiveURL)
	if err != nil {
		return nil, err
	}
	if err := verifyArchiveSHA256(download.SHA256Hex, sha); err != nil {
		download.Cleanup()
		return nil, err
	}
	if useCache {
		// The verified temp download is still usable; a write-only cache miss should not fail sync.
		_ = (archiveCache{dir: cacheDir}).Put(download.LocalPath, sha)
	}
	return download, nil
}

func normalizeArchiveSHA256(raw string) (string, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return "", false, nil
	}
	sha, ok := canonicalArchiveCacheSHA(raw)
	if !ok {
		return "", false, fmt.Errorf("invalid archive sha256 %q", raw)
	}
	return sha, true, nil
}

func verifyArchiveSHA256(actual, expected string) error {
	expectedSHA, hasExpectedSHA, err := normalizeArchiveSHA256(expected)
	if err != nil || !hasExpectedSHA {
		return err
	}
	actualSHA, ok := canonicalArchiveCacheSHA(actual)
	if !ok {
		return fmt.Errorf("invalid downloaded archive sha256 %q", actual)
	}
	if actualSHA != expectedSHA {
		return archiveDigestMismatchError{actual: actualSHA, expected: expectedSHA}
	}
	return nil
}

func canonicalArchiveCacheSHA(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) != 64 {
		return "", false
	}
	normalized := strings.ToLower(raw)
	for _, r := range normalized {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return "", false
		}
	}
	return normalized, true
}

func (c archiveCache) Get(expectedSHA string) (*providerpkg.DownloadResult, archiveCacheLookupResult, error) {
	sha, ok := canonicalArchiveCacheSHA(expectedSHA)
	if !ok {
		return nil, archiveCacheRejected, fmt.Errorf("invalid archive cache sha %q", expectedSHA)
	}
	path := c.pathForSHA(sha)
	cached, valid, err := copyCachedArchiveToTemp(path, sha)
	if os.IsNotExist(err) {
		return nil, archiveCacheMiss, nil
	}
	if err != nil {
		return nil, archiveCacheRejected, err
	}
	if !valid {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, archiveCacheRejected, fmt.Errorf("remove invalid cached archive: %w", err)
		}
		return nil, archiveCacheInvalid, nil
	}
	return cached, archiveCacheHit, nil
}

func (c archiveCache) Put(sourcePath, expectedSHA string) error {
	sha, ok := canonicalArchiveCacheSHA(expectedSHA)
	if !ok {
		return fmt.Errorf("invalid archive cache sha %q", expectedSHA)
	}

	targetPath := c.pathForSHA(sha)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create archive cache directory: %w", err)
	}
	if valid, err := validateCachedArchive(targetPath, sha); err == nil {
		if valid {
			return nil
		}
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove invalid cached archive: %w", err)
		}
	} else if !os.IsNotExist(err) {
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove invalid cached archive: %w", err)
		}
	}

	tmpPath, digest, cleanupTmp, err := copyRegularArchiveToTempWithDigest(
		sourcePath,
		"downloaded archive",
		filepath.Dir(targetPath),
		"."+filepath.Base(targetPath)+".tmp-*",
		providerpkg.MaxPackageBytes,
	)
	if err != nil {
		return err
	}
	keepTmp := false
	defer func() {
		if !keepTmp {
			cleanupTmp()
		}
	}()
	if digest != sha {
		return fmt.Errorf("archive cache temp file digest mismatch: got %s, want %s", digest, sha)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		if valid, validateErr := validateCachedArchive(targetPath, sha); validateErr == nil && valid {
			return nil
		}
		return fmt.Errorf("commit archive cache file: %w", err)
	}
	keepTmp = true
	return nil
}

func (c archiveCache) pathForSHA(sha string) string {
	return filepath.Join(c.dir, "sha256", sha[:2], sha+".tar.gz")
}

func validateCachedArchive(path, expectedSHA string) (bool, error) {
	_, digest, cleanup, err := copyRegularArchiveToTempWithDigest(
		path,
		"cached archive "+path,
		"",
		"gestalt-archive-cache-validate-*.tar.gz",
		providerpkg.MaxPackageBytes,
	)
	if err != nil {
		var sizeErr archiveSizeLimitError
		if errors.As(err, &sizeErr) {
			return false, nil
		}
		return false, err
	}
	cleanup()
	return digest == expectedSHA, nil
}

func copyCachedArchiveToTemp(path, expectedSHA string) (*providerpkg.DownloadResult, bool, error) {
	tmpPath, digest, cleanup, err := copyRegularArchiveToTempWithDigest(
		path,
		"cached archive "+path,
		"",
		"gestalt-archive-cache-*.tar.gz",
		providerpkg.MaxPackageBytes,
	)
	if err != nil {
		var sizeErr archiveSizeLimitError
		if errors.As(err, &sizeErr) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if digest != expectedSHA {
		cleanup()
		return nil, false, nil
	}
	return &providerpkg.DownloadResult{
		LocalPath: tmpPath,
		Cleanup:   cleanup,
		SHA256Hex: digest,
	}, true, nil
}

func copyRegularArchiveToTempWithDigest(sourcePath, subject, tempDir, tempPattern string, maxBytes int64) (string, string, func(), error) {
	info, err := statRegularArchiveFile(sourcePath, subject)
	if err != nil {
		return "", "", nil, err
	}
	if info.Size() > maxBytes {
		return "", "", nil, archiveSizeLimitError{subject: subject, maxBytes: maxBytes}
	}

	src, err := os.Open(sourcePath)
	if err != nil {
		return "", "", nil, fmt.Errorf("open %s: %w", subject, err)
	}
	defer func() {
		if src != nil {
			_ = src.Close()
		}
	}()
	openedInfo, err := src.Stat()
	if err != nil {
		return "", "", nil, fmt.Errorf("stat opened %s: %w", subject, err)
	}
	if !os.SameFile(info, openedInfo) {
		return "", "", nil, fmt.Errorf("%s %s changed during validation", subject, sourcePath)
	}

	tmp, err := os.CreateTemp(tempDir, tempPattern)
	if err != nil {
		return "", "", nil, fmt.Errorf("create archive temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanupTmp := func() { _ = os.Remove(tmpPath) }

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(src, maxBytes+1))
	if err != nil {
		_ = tmp.Close()
		cleanupTmp()
		return "", "", nil, fmt.Errorf("copy %s: %w", subject, err)
	}
	if n > maxBytes {
		_ = tmp.Close()
		cleanupTmp()
		return "", "", nil, archiveSizeLimitError{subject: subject, maxBytes: maxBytes}
	}
	if err := src.Close(); err != nil {
		src = nil
		_ = tmp.Close()
		cleanupTmp()
		return "", "", nil, fmt.Errorf("close %s: %w", subject, err)
	}
	src = nil
	if err := tmp.Close(); err != nil {
		cleanupTmp()
		return "", "", nil, fmt.Errorf("close archive temp file: %w", err)
	}
	return tmpPath, hex.EncodeToString(h.Sum(nil)), cleanupTmp, nil
}

func statRegularArchiveFile(path, subject string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is a symlink", subject)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", subject)
	}
	return info, nil
}
