package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

var (
	gestaltdBin string
	pluginBin   string
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "gestaltd-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}

	gestaltdBin = filepath.Join(tmpDir, "gestaltd")
	pluginBin = filepath.Join(tmpDir, "provider")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = buildGo(".", gestaltdBin) }()
	go func() {
		defer wg.Done()
		errs[1] = pluginpkg.BuildGoProviderBinary(testutil.MustExampleProviderPluginPath(), pluginBin, "provider-go", runtime.GOOS, runtime.GOARCH)
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			fmt.Fprintf(os.Stderr, "build %d: %v\n", i, err)
			_ = os.RemoveAll(tmpDir)
			os.Exit(1)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func buildGo(dir, output string) error {
	return runGo(dir, "build", "-o", output, ".")
}

func runGo(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
