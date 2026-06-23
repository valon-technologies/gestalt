package providerpkg

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const goReadonlyFlag = "-mod=readonly"

var (
	ErrNoGoProviderPackage = errors.New("no Go provider package found")
	ErrGoToolUnavailable   = errors.New("go tool unavailable")
)

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

// HasGoProviderPackage reports whether root looks like a Go source provider:
// `go list .` (which walks up to the nearest go.mod) resolves a package with
// .go files in root. The go.mod may live in root or an ancestor directory.
func HasGoProviderPackage(root string) bool {
	cmd := exec.Command("go", "list", goReadonlyFlag, "-f", "{{.ImportPath}} {{join .GoFiles \"|\"}}", ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	body := strings.TrimSpace(string(out))
	i := strings.IndexByte(body, ' ')
	if i < 0 {
		return false
	}
	return strings.TrimSpace(body[i+1:]) != ""
}

// goProviderServeCall maps a provider kind to the SDK serve call that runs the
// provider's New() constructor. Matches the kind-specific Serve<Kind>Provider
// surface in sdk/go.
func goProviderServeCall(kind string) (string, error) {
	switch providermanifestv1.NormalizeKind(kind) {
	case providermanifestv1.KindIdentity:
		return "gestalt.ServeIdentityProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindAuthorization:
		return "gestalt.ServeAuthorizationProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindExternalCredentials:
		return "gestalt.ServeExternalCredentialProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindIndexedDB:
		return "gestalt.ServeIndexedDBProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindCache:
		return "gestalt.ServeCacheProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindS3:
		return "gestalt.ServeS3Provider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindWorkflow:
		return "gestalt.ServeWorkflowProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindAgent:
		return "gestalt.ServeAgentProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindSecrets:
		return "gestalt.ServeSecretsProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindRuntime:
		return "gestalt.ServeRuntimeProvider(ctx, providerpkg.New())", nil
	default:
		return "", fmt.Errorf("unsupported Go provider kind %q", kind)
	}
}

// detectGoProviderImportPath resolves the import path of the Go package at
// root (the provider package itself, not a synthesized wrapper) via `go list`.
func detectGoProviderImportPath(root, goos, goarch string) (string, error) {
	cmd := exec.Command("go", "list", goReadonlyFlag, "-f", "{{.ImportPath}}", ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOOS="+goos, "GOARCH="+goarch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if bytes.Contains(stderr.Bytes(), []byte("no Go files")) {
			return "", ErrNoGoProviderPackage
		}
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrGoToolUnavailable
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
			return "", ErrNoGoProviderPackage
		}
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	defer func() { _ = file.Close() }()

	var moduleLine string
	for {
		var line string
		if _, scanErr := fmt.Fscanln(file, &line); scanErr != nil {
			break
		}
		if strings.HasPrefix(line, "module") {
			moduleLine = line
			break
		}
	}
	if moduleLine == "" {
		return "", fmt.Errorf("parse go.mod: module path not found")
	}
	fields := strings.Fields(moduleLine)
	if len(fields) < 2 {
		return "", fmt.Errorf("parse go.mod: module path is required")
	}
	return strings.Trim(fields[1], `"`), nil
}

// BuildGoProviderBinary synthesizes a wrapper main that serves the provider at
// root via the SDK Serve<Kind>Provider call, compiles it with `go build`, and
// writes the executable to outputPath. It is the implicit-build fallback used
// when a Go provider declares an entrypoint but no build.command.
func BuildGoProviderBinary(root, outputPath, kind, goos, goarch string) error {
	importPath, err := detectGoProviderImportPath(root, goos, goarch)
	if err != nil {
		return err
	}
	serveCall, err := goProviderServeCall(kind)
	if err != nil {
		return err
	}
	wrapper, cleanup, err := newGoWrapper("gestalt-go-provider-*.go", "Go provider wrapper", goExecutableWrapperTemplate, goExecutableWrapperData{
		ImportPath: importPath,
		ServeCall:  serveCall,
	})
	if err != nil {
		return err
	}
	defer cleanup()
	return buildGoWrapperBinary(root, outputPath, wrapper, goos, goarch)
}

func newGoWrapper(tempPattern, description string, tmpl *template.Template, data any) (string, func(), error) {
	file, err := os.CreateTemp("", tempPattern)
	if err != nil {
		return "", nil, fmt.Errorf("create %s: %w", description, err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	defer func() { _ = file.Close() }()

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("render %s: %w", description, err)
	}
	source, err := format.Source(buf.Bytes())
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("format %s: %w", description, err)
	}
	if _, err := file.Write(source); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write %s: %w", description, err)
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
