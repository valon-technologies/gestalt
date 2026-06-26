package testutil

import (
	"os"
	"path/filepath"
)

// fixtureDirEnv names a directory of prebuilt test fixture binaries. CI builds
// the expensive binaries once in a shared job and points the test jobs at the
// downloaded artifact via this variable, so each lane reuses them instead of
// rebuilding them in TestMain.
const fixtureDirEnv = "GESTALT_TEST_FIXTURE_DIR"

// PrebuiltBinary returns the path to a prebuilt fixture binary named `name`
// inside the directory named by GESTALT_TEST_FIXTURE_DIR, and true, when that
// directory is set and contains an executable file with that name. Otherwise it
// returns ("", false) and the caller should build the binary itself.
//
// The returned path lives outside any per-test temp dir, so callers must not
// remove or overwrite it; it is shared, read-only, and reused across packages.
func PrebuiltBinary(name string) (string, bool) {
	dir := os.Getenv(fixtureDirEnv)
	if dir == "" {
		return "", false
	}
	path := filepath.Join(dir, name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}
