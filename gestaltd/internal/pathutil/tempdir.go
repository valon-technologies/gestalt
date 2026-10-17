package pathutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveTempBaseDir selects or creates the first usable candidate directory.
func ResolveTempBaseDir(candidates []string, label string) (string, error) {
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
		return "", fmt.Errorf("resolve %s temp dir base: no directory candidates", label)
	}
	return "", fmt.Errorf("resolve %s temp dir base: %w", label, errors.Join(errs...))
}
