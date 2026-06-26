// Package main is the gestalt Go SDK command-line tool for provider build and run.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/template"

	"gopkg.in/yaml.v3"
)

const goReadonlyFlag = "-mod=readonly"

var (
	errNoGoProviderPackage = errors.New("no Go provider package found")
	errUsage               = errors.New("usage")
)

type manifest struct {
	Kind    string `yaml:"kind" json:"kind"`
	Source  string `yaml:"source" json:"source"`
	Version string `yaml:"version" json:"version"`
}

type goExecutableWrapperData struct {
	ImportPath string
	ServeCall  string
}

const goExecutableWrapperSource = `package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	providerpkg {{printf "%q" .ImportPath}}
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := {{.ServeCall}}; err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
`

var goExecutableWrapperTemplate = template.Must(template.New("go-executable-wrapper").Parse(goExecutableWrapperSource))

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errUsage) {
			fmt.Fprintf(os.Stderr, "usage: gestalt <build|run>\n")
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return errUsage
	}
	switch args[0] {
	case "build":
		return buildCommand()
	case "run":
		return runCommand()
	default:
		return errUsage
	}
}

func buildCommand() error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	manifest, err := readManifest(root)
	if err != nil {
		return err
	}
	goos := envOr("GESTALT_TARGET_OS", envOr("GOOS", runtime.GOOS))
	outputPath, err := sourceBuildOutputPath(manifest.Source, goos)
	if err != nil {
		return err
	}
	absOutput := filepath.Join(root, filepath.FromSlash(outputPath))
	if err := os.MkdirAll(filepath.Dir(absOutput), 0o755); err != nil {
		return fmt.Errorf("create build output directory: %w", err)
	}
	return buildGoProviderBinary(root, absOutput, manifest.Kind, goos, envOr("GESTALT_TARGET_ARCH", envOr("GOARCH", runtime.GOARCH)))
}

func runCommand() error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	manifest, err := readManifest(root)
	if err != nil {
		return err
	}
	wrapper, cleanup, err := newGoWrapper(root, manifest.Kind)
	if err != nil {
		return err
	}
	defer cleanup()

	binaryPath := filepath.Join(root, ".gestaltd", "run", "provider")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		return fmt.Errorf("create run output directory: %w", err)
	}
	goos := envOr("GESTALT_TARGET_OS", envOr("GOOS", runtime.GOOS))
	goarch := envOr("GESTALT_TARGET_ARCH", envOr("GOARCH", runtime.GOARCH))
	if err := buildGoWrapperBinary(root, binaryPath, wrapper, goos, goarch); err != nil {
		return err
	}
	return syscall.Exec(binaryPath, []string{filepath.Base(binaryPath)}, os.Environ())
}

func readManifest(root string) (manifest, error) {
	for _, name := range []string{"manifest.yaml", "manifest.yml", "manifest.json"} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return manifest{}, fmt.Errorf("read %s: %w", name, err)
		}
		var parsed manifest
		if strings.HasSuffix(name, ".json") {
			return manifest{}, fmt.Errorf("json manifests are not supported by gestalt go cli yet")
		}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return manifest{}, fmt.Errorf("parse %s: %w", name, err)
		}
		if strings.TrimSpace(parsed.Kind) == "" {
			return manifest{}, fmt.Errorf("%s: kind is required", name)
		}
		if strings.TrimSpace(parsed.Source) == "" {
			return manifest{}, fmt.Errorf("%s: source is required", name)
		}
		return parsed, nil
	}
	return manifest{}, fmt.Errorf("no manifest file found in %s", root)
}

func sourceBuildOutputPath(source, goos string) (string, error) {
	name := sourceAppName(source)
	base := filepath.ToSlash(filepath.Join(".gestaltd", "bin", name))
	if goos == "windows" {
		return base + ".exe", nil
	}
	return base, nil
}

func sourceAppName(source string) string {
	parts := strings.Split(strings.TrimSpace(source), "/")
	if len(parts) == 0 {
		return "provider"
	}
	return parts[len(parts)-1]
}

func goProviderServeCall(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "identity":
		return "gestalt.ServeIdentityProvider(ctx, providerpkg.New())", nil
	case "authorization":
		return "gestalt.ServeAuthorizationProvider(ctx, providerpkg.New())", nil
	case "externalcredentials":
		return "gestalt.ServeExternalCredentialProvider(ctx, providerpkg.New())", nil
	case "indexeddb":
		return "gestalt.ServeIndexedDBProvider(ctx, providerpkg.New())", nil
	case "cache":
		return "gestalt.ServeCacheProvider(ctx, providerpkg.New())", nil
	case "s3":
		return "gestalt.ServeS3Provider(ctx, providerpkg.New())", nil
	case "workflow":
		return "gestalt.ServeWorkflowProvider(ctx, providerpkg.New())", nil
	case "agent":
		return "gestalt.ServeAgentProvider(ctx, providerpkg.New())", nil
	case "secrets":
		return "gestalt.ServeSecretsProvider(ctx, providerpkg.New())", nil
	case "runtime":
		return "gestalt.ServeRuntimeProvider(ctx, providerpkg.New())", nil
	default:
		return "", fmt.Errorf("unsupported Go provider kind %q", kind)
	}
}

func detectGoProviderImportPath(root, goos, goarch string) (string, error) {
	cmd := exec.Command("go", "list", goReadonlyFlag, "-f", "{{.ImportPath}}", ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOOS="+goos, "GOARCH="+goarch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if bytes.Contains(stderr.Bytes(), []byte("no Go files")) {
			return "", errNoGoProviderPackage
		}
		return "", fmt.Errorf("go list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	importPath := strings.TrimSpace(string(out))
	if importPath == "" || importPath == "." {
		return goModulePathFromFile(root)
	}
	return importPath, nil
}

func goModulePathFromFile(root string) (string, error) {
	file, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", errNoGoProviderPackage
		}
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return "", fmt.Errorf("parse go.mod: module path is required")
		}
		return strings.Trim(fields[1], `"`), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	return "", fmt.Errorf("parse go.mod: module path not found")
}

func buildGoProviderBinary(root, outputPath, kind, goos, goarch string) error {
	importPath, err := detectGoProviderImportPath(root, goos, goarch)
	if err != nil {
		return err
	}
	serveCall, err := goProviderServeCall(kind)
	if err != nil {
		return err
	}
	wrapper, cleanup, err := newGoWrapperFile(root, kind, importPath, serveCall)
	if err != nil {
		return err
	}
	defer cleanup()
	return buildGoWrapperBinary(root, outputPath, wrapper, goos, goarch)
}

func newGoWrapper(root, kind string) (string, func(), error) {
	importPath, err := detectGoProviderImportPath(root, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", nil, err
	}
	serveCall, err := goProviderServeCall(kind)
	if err != nil {
		return "", nil, err
	}
	return newGoWrapperFile(root, kind, importPath, serveCall)
}

func newGoWrapperFile(root, kind, importPath, serveCall string) (string, func(), error) {
	file, err := os.CreateTemp("", "gestalt-go-provider-*.go")
	if err != nil {
		return "", nil, fmt.Errorf("create Go provider wrapper: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	defer func() { _ = file.Close() }()

	var buf bytes.Buffer
	if err := goExecutableWrapperTemplate.Execute(&buf, goExecutableWrapperData{
		ImportPath: importPath,
		ServeCall:  serveCall,
	}); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("render Go provider wrapper: %w", err)
	}
	source, err := format.Source(buf.Bytes())
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("format Go provider wrapper: %w", err)
	}
	if _, err := file.Write(source); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write Go provider wrapper: %w", err)
	}
	return path, cleanup, nil
}

func buildGoWrapperBinary(root, outputPath, wrapperPath, goos, goarch string) error {
	cmd := exec.Command("go", "-C", root, "build", goReadonlyFlag, "-trimpath", "-ldflags", "-s -w", "-o", outputPath, wrapperPath)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off", "GOOS="+goos, "GOARCH="+goarch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
