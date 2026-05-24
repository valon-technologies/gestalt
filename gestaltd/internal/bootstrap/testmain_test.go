package bootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

var (
	sharedEchoPluginBin      string
	sharedExampleProviderBin string
	sharedAgentProviderBin   string
	sharedGestaltdBin        string
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "bootstrap-test-binaries-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}

	sharedEchoPluginBin = filepath.Join(tmpDir, "gestalt-app-echo")
	sharedExampleProviderBin = filepath.Join(tmpDir, "provider-go")
	sharedAgentProviderBin = filepath.Join(tmpDir, "gestalt-agent-test")
	sharedGestaltdBin = filepath.Join(tmpDir, "gestaltd")

	root, err := repoRootForBootstrapTests()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve repo root: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	type buildSpec struct {
		name      string
		dir       string
		target    string
		output    string
		sdkModule bool
	}

	specs := []buildSpec{
		{
			name:      "echo plugin",
			dir:       testutil.MustSDKTestProviderPath("echo"),
			output:    sharedEchoPluginBin,
			sdkModule: true,
		},
		{
			name:   "example provider",
			dir:    testutil.MustExampleProviderPluginPath(),
			target: "",
			output: sharedExampleProviderBin,
		},
		{
			name:      "agent provider",
			dir:       testutil.MustSDKTestProviderPath("agent"),
			output:    sharedAgentProviderBin,
			sdkModule: true,
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
			errs[i] = buildBootstrapTestBinary(specs[i].dir, specs[i].target, specs[i].output, specs[i].sdkModule)
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

func buildBootstrapTestBinary(dir, target, output string, sdkModule bool) error {
	if sdkModule {
		return testutil.BuildSDKTestMainBinary(dir, output)
	}
	if target == "" {
		return buildBootstrapGoProviderFixture(dir, output)
	}
	return runGoCommand(dir, "build", "-o", output, target)
}

func buildBootstrapGoProviderFixture(dir, output string) error {
	buildDir, err := os.MkdirTemp("", "bootstrap-provider-*")
	if err != nil {
		return err
	}
	if err := copyBootstrapFixtureTree(dir, buildDir); err != nil {
		return err
	}
	goModPath := filepath.Join(buildDir, "go.mod")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		return err
	}
	root, err := repoRootForBootstrapTests()
	if err != nil {
		return err
	}
	replaced := strings.Replace(string(goMod), "replace github.com/valon-technologies/gestalt/sdk/go => ../../../../../sdk/go", "replace github.com/valon-technologies/gestalt/sdk/go => "+filepath.Join(root, "sdk", "go"), 1)
	replaced = strings.Replace(replaced, "replace github.com/valon-technologies/gestalt/server/rpc => ../../../../../gestaltd/rpc", "replace github.com/valon-technologies/gestalt/server/rpc => "+filepath.Join(root, "gestaltd", "rpc"), 1)
	if err := os.WriteFile(goModPath, []byte(replaced), 0o644); err != nil {
		return err
	}
	mainDir := filepath.Join(buildDir, "cmd", "provider")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		return err
	}
	mainSource := fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	providerpkg %q
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeProvider(ctx, providerpkg.New(), providerpkg.Router.WithName(%q)); err != nil {
		fmt.Fprintf(os.Stderr, "error: %%v\n", err)
		os.Exit(1)
	}
}
`, "github.com/valon-technologies/gestalt/testdata/provider-go", filepath.Base(dir))
	if err := os.WriteFile(filepath.Join(mainDir, "main.go"), []byte(mainSource), 0o644); err != nil {
		return err
	}
	return runGoCommand(buildDir, "build", "-o", output, "./cmd/provider")
}

func copyBootstrapFixtureTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
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
