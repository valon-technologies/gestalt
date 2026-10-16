package pathutil

import (
	"path/filepath"
	"strings"
)

// IsWithin reports whether target is root or a lexical descendant of root.
// It does not resolve symlinks and returns false when paths cannot be compared.
func IsWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
