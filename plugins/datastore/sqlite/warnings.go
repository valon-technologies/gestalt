package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *Store) Warnings() []string {
	if isTempPath(s.path) {
		return []string{fmt.Sprintf(
			"sqlite datastore path %q uses temporary storage; data will be lost on restart. Use a mounted persistent volume or switch to a shared datastore such as postgres.",
			s.path,
		)}
	}
	if !filepath.IsAbs(s.path) {
		return []string{fmt.Sprintf(
			"sqlite datastore path %q uses local filesystem storage; in a container this path is ephemeral. Use an absolute path on a mounted persistent volume or switch to a shared datastore such as postgres.",
			s.path,
		)}
	}
	return nil
}

func isTempPath(path string) bool {
	clean := filepath.Clean(path)
	tempPrefixes := []string{
		"/tmp",
		"/var/tmp",
		filepath.Clean(os.TempDir()),
	}
	for _, prefix := range tempPrefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
