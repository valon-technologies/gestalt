package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

// Builds only the provider fixtures the in-process lock/sync tests need. The
// e2e suite (which also builds and execs the gestaltd binary) has its own TestMain.
var (
	indexedDBBin           string
	externalCredentialsBin string
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "gestaltd-unit-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}

	indexedDBBin = filepath.Join(tmpDir, "indexeddb-provider")
	externalCredentialsBin = filepath.Join(tmpDir, "external-credentials-provider")
	indexedDBSrcDir := filepath.Join(filepath.Dir(testutil.MustExampleProviderAppPath()), "provider-go-indexeddb")
	externalCredentialsSrcDir, err := writeExternalCredentialsProviderFixture(tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write external credentials fixture: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = buildGoFixtureBinary(indexedDBSrcDir, indexedDBBin, "github.com/valon-technologies/gestalt/testdata/provider-go-indexeddb", "gestalt.ServeIndexedDBProvider(ctx, providerpkg.New())")
	}()
	go func() {
		defer wg.Done()
		errs[1] = buildGoFixtureBinary(externalCredentialsSrcDir, externalCredentialsBin, "github.com/valon-technologies/gestalt/testdata/provider-go-externalcredentials", "gestalt.ServeExternalCredentialProvider(ctx, providerpkg.New())")
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

func buildGoFixtureBinary(srcDir, output, importPath, serveCall string) error {
	buildDir, err := os.MkdirTemp(filepath.Dir(output), "go-provider-fixture-*")
	if err != nil {
		return err
	}
	if err := copyTestFixtureTree(srcDir, buildDir); err != nil {
		return err
	}
	goModPath := filepath.Join(buildDir, "go.mod")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		return err
	}
	root := filepath.Clean(filepath.Join(testutil.MustExampleProviderPluginPath(), "..", "..", "..", "..", ".."))
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
	if err := %s; err != nil {
		fmt.Fprintf(os.Stderr, "error: %%v\n", err)
		os.Exit(1)
	}
}
`, importPath, serveCall)
	if err := os.WriteFile(filepath.Join(mainDir, "main.go"), []byte(mainSource), 0o644); err != nil {
		return err
	}
	return runGo(buildDir, "build", "-o", output, "./cmd/provider")
}

func copyTestFixtureTree(src, dst string) error {
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

func runGo(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeExternalCredentialsProviderFixture(baseDir string) (string, error) {
	fixtureDir := filepath.Join(baseDir, "external-credentials-fixture")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		return "", err
	}

	exampleDir := testutil.MustExampleProviderPluginPath()
	goModPath := filepath.Join(exampleDir, "go.mod")
	goSumPath := filepath.Join(exampleDir, "go.sum")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}
	root := filepath.Clean(filepath.Join(exampleDir, "..", "..", "..", "..", ".."))
	replaced := strings.Replace(string(goMod), "module github.com/valon-technologies/gestalt/testdata/provider-go", "module github.com/valon-technologies/gestalt/testdata/provider-go-externalcredentials", 1)
	replaced = strings.Replace(replaced, "replace github.com/valon-technologies/gestalt/sdk/go => ../../../../../sdk/go", "replace github.com/valon-technologies/gestalt/sdk/go => "+filepath.Join(root, "sdk", "go"), 1)
	replaced = strings.Replace(replaced, "replace github.com/valon-technologies/gestalt/server/rpc => ../../../../../gestaltd/rpc", "replace github.com/valon-technologies/gestalt/server/rpc => "+filepath.Join(root, "gestaltd", "rpc"), 1)
	if err := os.WriteFile(filepath.Join(fixtureDir, "go.mod"), []byte(replaced), 0o644); err != nil {
		return "", err
	}

	goSum, err := os.ReadFile(goSumPath)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "go.sum"), goSum, 0o644); err != nil {
		return "", err
	}

	if err := os.WriteFile(filepath.Join(fixtureDir, "externalcredentials.go"), []byte(testutil.GeneratedExternalCredentialPackageSource()), 0o644); err != nil {
		return "", err
	}

	return fixtureDir, nil
}
