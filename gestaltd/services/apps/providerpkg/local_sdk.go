package providerpkg

import (
	"os"
	"path/filepath"
	"runtime"
)

func localRepositorySubdir(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "gestaltd", "go.mod")); err == nil {
			path := filepath.Join(append([]string{dir}, parts...)...)
			if _, err := os.Stat(path); err == nil {
				return filepath.Clean(path)
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
	}
}
