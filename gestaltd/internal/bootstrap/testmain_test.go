package bootstrap

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
	sharedEchoPluginBin      string
	sharedExampleProviderBin string
	sharedGestaltdBin        string
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "bootstrap-test-binaries-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}

	sharedEchoPluginBin = filepath.Join(tmpDir, "gestalt-plugin-echo")
	sharedExampleProviderBin = filepath.Join(tmpDir, "provider-go")
	sharedGestaltdBin = filepath.Join(tmpDir, "gestaltd")

	root, err := repoRootForBootstrapTests()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve repo root: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	type buildSpec struct {
		name   string
		dir    string
		target string
		output string
	}

	specs := []buildSpec{
		{
			name:   "echo plugin",
			dir:    filepath.Join(root, "gestaltd"),
			target: "./internal/testplugins/echo",
			output: sharedEchoPluginBin,
		},
		{
			name:   "example provider",
			dir:    testutil.MustExampleProviderPluginPath(),
			target: "",
			output: sharedExampleProviderBin,
		},
		{
			name:   "gestaltd",
			dir:    filepath.Join(root, "gestaltd"),
			target: "./cmd/gestaltd",
			output: sharedGestaltdBin,
		},
	}

	errs := make([]error, len(specs))
	var wg sync.WaitGroup
	wg.Add(len(specs))
	for i := range specs {
		i := i
		go func() {
			defer wg.Done()
			errs[i] = buildBootstrapTestBinary(specs[i].dir, specs[i].target, specs[i].output)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			fmt.Fprintf(os.Stderr, "build %s: %v\n", specs[i].name, err)
			_ = os.RemoveAll(tmpDir)
			os.Exit(1)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func buildBootstrapTestBinary(dir, target, output string) error {
	if target == "" {
		return pluginpkg.BuildGoProviderBinary(dir, output, filepath.Base(dir), runtime.GOOS, runtime.GOARCH)
	}
	return runGoCommand(dir, "build", "-o", output, target)
}

func runGoCommand(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func repoRootForBootstrapTests() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), nil
}
