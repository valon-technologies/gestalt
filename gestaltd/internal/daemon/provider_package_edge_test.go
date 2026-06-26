package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderPackagePreservesOtherPlatformArchives(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	linuxArchive := platformArchiveNameForTest(releaseTestAppName, "1.0.0", "linux", "amd64")
	darwinArchive := platformArchiveNameForTest(releaseTestAppName, "0.9.0", "darwin", "arm64")
	writeTestFile(t, outputDir, linuxArchive, []byte("linux archive"), 0o644)
	writeTestFile(t, outputDir, darwinArchive, []byte("stale darwin archive"), 0o644)

	err := removeStalePackageArchives(outputDir, releaseTestAppName, releaseArchiveTargets([]releasePlatform{{GOOS: "darwin", GOARCH: "arm64"}}))
	if err != nil {
		t.Fatalf("removeStalePackageArchives: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, linuxArchive)); err != nil {
		t.Fatalf("expected other platform archive %s to remain: %v", linuxArchive, err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, darwinArchive)); !os.IsNotExist(err) {
		t.Fatalf("expected same platform archive %s to be removed, got err=%v", darwinArchive, err)
	}
}
