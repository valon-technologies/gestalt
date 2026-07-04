package providerpkg

import (
	"fmt"
	"os"
	"path/filepath"
)

const sourceStaticBuildDirName = ".gestalt/build-static"
const envGestaltBuildStatic = "GESTALT_BUILD_STATIC"

// SourceStaticBuildDir is the absolute path where build commands write static assets.
func SourceStaticBuildDir(manifestPath string) string {
	return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(sourceStaticBuildDirName))
}

func sourceStaticBuildDirRel() string {
	return sourceStaticBuildDirName
}

func sourceStaticQualifies(staticDir string) (bool, error) {
	info, err := os.Stat(staticDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	indexPath := filepath.Join(staticDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	entries, err := os.ReadDir(staticDir)
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

func prepareSourceStaticBuildDir(manifestPath string) (string, error) {
	staticDir := SourceStaticBuildDir(manifestPath)
	if err := os.RemoveAll(staticDir); err != nil {
		return "", fmt.Errorf("remove static build dir: %w", err)
	}
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		return "", fmt.Errorf("create static build dir: %w", err)
	}
	abs, err := filepath.Abs(staticDir)
	if err != nil {
		return "", fmt.Errorf("resolve static build dir: %w", err)
	}
	return abs, nil
}

func verifySourceStaticBuildOutput(staticDir string) error {
	ok, err := sourceStaticQualifies(staticDir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("static build output missing index.html under %q", sourceStaticBuildDirRel())
	}
	return nil
}
