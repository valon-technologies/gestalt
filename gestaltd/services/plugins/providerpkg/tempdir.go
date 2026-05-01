package providerpkg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolvePackageTempBaseDir(candidates []string) (string, error) {
	seen := make(map[string]struct{}, len(candidates))
	var errs []error
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if !filepath.IsAbs(candidate) {
			abs, err := filepath.Abs(candidate)
			if err == nil {
				candidate = abs
			}
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		info, err := os.Stat(candidate)
		switch {
		case err == nil && info.IsDir():
			return candidate, nil
		case err == nil:
			errs = append(errs, fmt.Errorf("%s exists but is not a directory", candidate))
		case os.IsNotExist(err):
			if mkErr := os.MkdirAll(candidate, 0o755); mkErr == nil {
				return candidate, nil
			} else {
				errs = append(errs, fmt.Errorf("mkdir %s: %w", candidate, mkErr))
			}
		default:
			errs = append(errs, fmt.Errorf("stat %s: %w", candidate, err))
		}
	}
	if len(errs) == 0 {
		return "", fmt.Errorf("resolve package temp dir base: no directory candidates")
	}
	return "", fmt.Errorf("resolve package temp dir base: %w", errors.Join(errs...))
}

func createPackageTempFile(pattern string) (*os.File, error) {
	base, err := resolvePackageTempBaseDir([]string{"/tmp", os.TempDir(), "/var/tmp", "/dev/shm", "."})
	if err != nil {
		return nil, fmt.Errorf("resolve package temp dir base: %w", err)
	}
	file, err := os.CreateTemp(base, pattern)
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	return file, nil
}
