package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

var (
	gestaltdBin   string
	pluginBin     string
	indexedDBBin  string
	gestaltCLIBin string
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "gestaltd-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}

	gestaltdBin = filepath.Join(tmpDir, "gestaltd")
	pluginBin = filepath.Join(tmpDir, "provider")
	indexedDBBin = filepath.Join(tmpDir, "indexeddb-provider")
	indexedDBSrcDir := filepath.Join(filepath.Dir(testutil.MustExampleProviderPluginPath()), "provider-go-indexeddb")

	var wg sync.WaitGroup
	errs := make([]error, 3)
	wg.Add(3)
	go func() { defer wg.Done(); errs[0] = buildTarget(".", ".", gestaltdBin) }()
	go func() {
		defer wg.Done()
		errs[1] = providerpkg.BuildGoProviderBinary(testutil.MustExampleProviderPluginPath(), pluginBin, "provider-go", runtime.GOOS, runtime.GOARCH)
	}()
	go func() {
		defer wg.Done()
		errs[2] = providerpkg.BuildGoComponentBinary(indexedDBSrcDir, indexedDBBin, "indexeddb", runtime.GOOS, runtime.GOARCH)
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			fmt.Fprintf(os.Stderr, "build %d: %v\n", i, err)
			_ = os.RemoveAll(tmpDir)
			os.Exit(1)
		}
	}

	gestaltCLIBin = buildGestaltCLI()

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func buildGestaltCLI() string {
	if _, err := exec.LookPath("cargo"); err != nil {
		return ""
	}

	repoRoot := filepath.Dir(filepath.Dir(testutil.MustExampleProviderPluginPath()))
	for {
		if _, err := os.Stat(filepath.Join(repoRoot, "gestalt", "Cargo.toml")); err == nil {
			break
		}
		parent := filepath.Dir(repoRoot)
		if parent == repoRoot {
			return ""
		}
		repoRoot = parent
	}

	workspaceDir := filepath.Join(repoRoot, "gestalt")
	cmd := exec.Command("cargo", "build", "-p", "gestalt", "--release")
	cmd.Dir = workspaceDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build gestalt CLI: %v (skipping CLI tests)\n", err)
		return ""
	}

	builtBin := filepath.Join(workspaceDir, "target", "release", "gestalt")
	if _, err := os.Stat(builtBin); err != nil {
		fmt.Fprintf(os.Stderr, "gestalt CLI binary not found at %s\n", builtBin)
		return ""
	}
	return builtBin
}

func buildTarget(dir, target, output string) error {
	return runGo(dir, "build", "-o", output, target)
}

func runGo(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
