package app_publish

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var gestaltdBin string

func TestMain(m *testing.M) {
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve user cache dir: %v\n", err)
		os.Exit(1)
	}
	cacheDir := filepath.Join(cacheRoot, "valon-gestaltd-app-publish-test")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create cache dir: %v\n", err)
		os.Exit(1)
	}

	gestaltdBin = filepath.Join(cacheDir, "gestaltd")
	if err := buildGestaltd(gestaltdBin); err != nil {
		fmt.Fprintf(os.Stderr, "build gestaltd: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func buildGestaltd(output string) error {
	cmd := exec.Command("go", "build", "-o", output, "github.com/valon-technologies/gestalt/server/cmd/gestaltd")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
