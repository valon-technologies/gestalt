package operator

import (
	"fmt"
	"os"
	"strings"
)

type archiveDigestMismatchError struct {
	actual   string
	expected string
}

func (e archiveDigestMismatchError) Error() string {
	return fmt.Sprintf("archive digest mismatch: got %s, want %s", e.actual, e.expected)
}

func normalizeArchiveSHA256(raw string) (string, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return "", false, nil
	}
	sha, ok := canonicalArchiveSHA256(raw)
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
	actualSHA, ok := canonicalArchiveSHA256(actual)
	if !ok {
		return fmt.Errorf("invalid downloaded archive sha256 %q", actual)
	}
	if actualSHA != expectedSHA {
		return archiveDigestMismatchError{actual: actualSHA, expected: expectedSHA}
	}
	return nil
}

func canonicalArchiveSHA256(raw string) (string, bool) {
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

func archiveFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
