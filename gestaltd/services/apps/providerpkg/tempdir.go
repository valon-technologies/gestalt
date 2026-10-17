package providerpkg

import (
	"fmt"
	"os"

	"github.com/valon-technologies/gestalt/server/internal/pathutil"
)

func createPackageTempFile(pattern string) (*os.File, error) {
	base, err := pathutil.ResolveTempBaseDir([]string{"/tmp", os.TempDir(), "/var/tmp", "/dev/shm", "."}, "package")
	if err != nil {
		return nil, fmt.Errorf("resolve package temp dir base: %w", err)
	}
	file, err := os.CreateTemp(base, pattern)
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	return file, nil
}
