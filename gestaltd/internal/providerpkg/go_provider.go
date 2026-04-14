package providerpkg

import (
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"text/template"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const goProviderPackageTarget = "."
const goReadonlyFlag = "-mod=readonly"

var ErrNoGoProviderPackage = errors.New("no Go provider package found")
var ErrNoSourceComponentPackage = errors.New("no source component package found")
var ErrGoToolUnavailable = errors.New("go tool unavailable")
var goProviderNameSlugPattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

type goExecutableWrapperData struct {
	ImportPath string
	ServeCall  string
}

//go:embed go_executable_wrapper.go.tmpl
var goExecutableWrapperSource string

var goExecutableWrapperTemplate = template.Must(template.New("go-executable-wrapper").Parse(goExecutableWrapperSource))

func DetectGoProviderImportPath(root, goos, goarch string) (string, error) {
	return detectGoPackageImportPath(root, goProviderPackageTarget, ErrNoGoProviderPackage, goProviderSourceExists, goos, goarch)
}

func BuildGoProviderTempBinary(root, goos, goarch string) (string, func(), error) {
	pluginName := sourcePluginName(root)
	return buildGoTempBinary("gestalt-go-provider-bin-*", "provider", goos, func(outputPath string) error {
		return BuildGoProviderBinary(root, outputPath, pluginName, goos, goarch)
	})
}

func BuildGoProviderBinary(root, outputPath, pluginName, goos, goarch string) error {
	return buildGoSourceBinary(root, outputPath, goos, goarch, ErrNoGoProviderPackage, goProviderSourceExists, "gestalt-go-provider-*.go", "Go provider wrapper", func(importPath string) (goExecutableWrapperData, error) {
		return goExecutableWrapperData{
			ImportPath: importPath,
			ServeCall:  fmt.Sprintf("gestalt.ServeProvider(ctx, providerpkg.New(), providerpkg.Router.WithName(%q))", pluginName),
		}, nil
	})
}

func DetectGoComponentImportPath(root, kind, goos, goarch string) (string, error) {
	if err := validateSourceComponentKind(kind); err != nil {
		return "", err
	}
	return detectGoPackageImportPath(root, goProviderPackageTarget, ErrNoSourceComponentPackage, goComponentSourceExists, goos, goarch)
}

func BuildGoComponentTempBinary(root, kind, goos, goarch string) (string, func(), error) {
	return buildGoTempBinary("gestalt-go-component-bin-*", kind, goos, func(outputPath string) error {
		return BuildGoComponentBinary(root, outputPath, kind, goos, goarch)
	})
}

func BuildGoComponentBinary(root, outputPath, kind, goos, goarch string) error {
	if err := validateSourceComponentKind(kind); err != nil {
		return err
	}
	return buildGoSourceBinary(root, outputPath, goos, goarch, ErrNoSourceComponentPackage, goComponentSourceExists, "gestalt-go-component-*.go", fmt.Sprintf("Go %s wrapper", kind), func(importPath string) (goExecutableWrapperData, error) {
		serveCall, err := componentServeCall(kind)
		if err != nil {
			return goExecutableWrapperData{}, err
		}
		return goExecutableWrapperData{
			ImportPath: importPath,
			ServeCall:  serveCall,
		}, nil
	})
}

func SourceComponentExecutionCommand(root, kind, goos, goarch string) (string, []string, func(), error) {
	sourceKind, target, err := detectSourceComponent(root, kind, goos, goarch)
	if err != nil {
		return "", nil, nil, err
	}
	switch sourceKind {
	case sourceProviderKindGo:
		command, cleanup, err := BuildGoComponentTempBinary(root, kind, goos, goarch)
		if err != nil {
			if errors.Is(err, ErrNoSourceComponentPackage) {
				return "", nil, nil, err
			}
			return "", nil, nil, fmt.Errorf("build source %s temp binary: %w", kind, err)
		}
		return command, nil, cleanup, nil
	case sourceProviderKindRust:
		return rustComponentExecutionCommand(root, kind, goos, goarch)
	case sourceProviderKindPython:
		runtimeKind, err := pythonRuntimeKind(kind)
		if err != nil {
			return "", nil, nil, err
		}
		return pythonComponentExecutionCommand(root, target, runtimeKind)
	case sourceProviderKindTypeScript:
		return typeScriptExecutionCommand(root, target)
	default:
		return "", nil, nil, ErrNoSourceComponentPackage
	}
}

func SourceComponentExecutionEnv(root, kind, goos, goarch string) (map[string]string, error) {
	sourceKind, _, err := detectSourceComponent(root, kind, goos, goarch)
	if err != nil {
		return nil, err
	}
	if sourceKind != sourceProviderKindPython {
		return nil, nil
	}
	return pythonBackendEnvMap(), nil
}

func ValidateSourceComponentRelease(root, kind, goos, goarch string) error {
	sourceKind, _, err := detectSourceComponent(root, kind, goos, goarch)
	if err != nil {
		return err
	}
	switch sourceKind {
	case sourceProviderKindPython:
		_, err = DetectPythonInterpreter(root, goos, goarch)
		return err
	case sourceProviderKindRust:
		return ValidateRustComponentRelease(root, kind, goos, goarch)
	case sourceProviderKindTypeScript:
		_, err = DetectBunExecutable()
		return err
	default:
		return nil
	}
}

func BuildSourceComponentReleaseBinary(root, outputPath, kind, goos, goarch string) (string, error) {
	sourceKind, target, err := detectSourceComponent(root, kind, goos, goarch)
	if err != nil {
		return "", err
	}
	switch sourceKind {
	case sourceProviderKindGo:
		return "", BuildGoComponentBinary(root, outputPath, kind, goos, goarch)
	case sourceProviderKindRust:
		return BuildRustComponentBinary(root, outputPath, kind, goos, goarch)
	case sourceProviderKindPython:
		runtimeKind, err := pythonRuntimeKind(kind)
		if err != nil {
			return "", err
		}
		pluginName := sourcePluginName(root)
		return BuildPythonComponentBinary(root, outputPath, pluginName, target, runtimeKind, goos, goarch)
	case sourceProviderKindTypeScript:
		return BuildTypeScriptComponentBinary(root, outputPath, kind, target, goos, goarch)
	default:
		return "", ErrNoSourceComponentPackage
	}
}

func HasSourceComponentPackage(root, kind string) (bool, error) {
	_, _, err := detectSourceComponent(root, kind, runtime.GOOS, runtime.GOARCH)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNoSourceComponentPackage):
		return false, nil
	default:
		return false, err
	}
}

func detectSourceComponent(root, kind, goos, goarch string) (sourceKind string, target string, err error) {
	if err := validateSourceComponentKind(kind); err != nil {
		return "", "", err
	}

	var goToolUnavailable error
	if _, err := DetectGoComponentImportPath(root, kind, goos, goarch); err == nil {
		return sourceProviderKindGo, "", nil
	} else if errors.Is(err, ErrGoToolUnavailable) {
		goToolUnavailable = err
	} else if !errors.Is(err, ErrNoSourceComponentPackage) {
		return "", "", err
	}
	if _, err := detectRustPackage(root); err == nil {
		return sourceProviderKindRust, "", nil
	} else if !errors.Is(err, ErrNoRustProviderPackage) {
		return "", "", err
	}

	target, err = DetectPythonComponentTarget(root, kind)
	switch {
	case err == nil:
		return sourceProviderKindPython, target, nil
	case !errors.Is(err, ErrNoPythonSourceComponentPackage):
		return "", "", err
	default:
		target, err = DetectTypeScriptComponentTarget(root, kind)
		switch {
		case err == nil:
			return sourceProviderKindTypeScript, target, nil
		case !errors.Is(err, ErrNoSourceComponentPackage):
			return "", "", err
		case goToolUnavailable != nil:
			return "", "", goToolUnavailable
		default:
			return "", "", ErrNoSourceComponentPackage
		}
	}
}

func detectGoPackageImportPath(root, buildTarget string, noSourceErr error, sourceExists func(string) bool, goos, goarch string) (string, error) {
	importPath, err := goPackageField(root, buildTarget, "{{.ImportPath}}", goos, goarch)
	if err != nil {
		if errors.Is(err, ErrGoToolUnavailable) {
			if !sourceExists(root) {
				return "", noSourceErr
			}
			return "", err
		}
		if isMissingGoPackageError(err) {
			return "", noSourceErr
		}
		return "", fmt.Errorf("%s: %w", buildTarget, err)
	}
	importPath = strings.TrimSpace(importPath)
	if importPath != "" && importPath != "." {
		return importPath, nil
	}

	fallbackModulePath, moduleErr := goModulePathFromFile(root)
	if moduleErr != nil {
		return "", moduleErr
	}
	modulePath, err := goPackageField(root, buildTarget, "{{if .Module}}{{.Module.Path}}{{end}}", goos, goarch)
	if err == nil {
		modulePath = strings.TrimSpace(modulePath)
		if modulePath != "" {
			return modulePath, nil
		}
	}
	if fallbackModulePath != "" {
		return fallbackModulePath, nil
	}
	return importPath, nil
}

func goModulePathFromFile(root string) (string, error) {
	file, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(stripGoModComment(scanner.Text()))
		if line == "" || !strings.HasPrefix(line, "module") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return "", fmt.Errorf("parse go.mod: module path is required")
		}
		return strings.Trim(fields[1], `"`), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan go.mod: %w", err)
	}
	return "", nil
}

func stripGoModComment(line string) string {
	if idx := strings.Index(line, "//"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func newGoSourceWrapper(root, goos, goarch string, noSourceErr error, sourceExists func(string) bool, tempPattern, description string, wrapperData func(string) (goExecutableWrapperData, error)) (string, func(), error) {
	importPath, err := detectGoPackageImportPath(root, goProviderPackageTarget, noSourceErr, sourceExists, goos, goarch)
	if err != nil {
		return "", nil, err
	}
	data, err := wrapperData(importPath)
	if err != nil {
		return "", nil, err
	}
	return newGoWrapper(tempPattern, description, goExecutableWrapperTemplate, data)
}

func buildGoSourceBinary(root, outputPath, goos, goarch string, noSourceErr error, sourceExists func(string) bool, tempPattern, description string, wrapperData func(string) (goExecutableWrapperData, error)) error {
	wrapperPath, cleanup, err := newGoSourceWrapper(root, goos, goarch, noSourceErr, sourceExists, tempPattern, description, wrapperData)
	if err != nil {
		return err
	}
	defer cleanup()

	return buildGoWrapperBinary(root, outputPath, wrapperPath, goos, goarch)
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

func buildGoTempBinary(tempDirPattern, binaryBaseName, goos string, build func(outputPath string) error) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", tempDirPattern)
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }

	binaryName := binaryBaseName
	if goos == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(tempDir, binaryName)
	if err := build(binaryPath); err != nil {
		cleanup()
		return "", nil, err
	}
	return binaryPath, cleanup, nil
}

func buildGoWrapperBinary(root, outputPath, wrapperPath, goos, goarch string) error {
	cmd := exec.Command("go", "-C", root, "build", goReadonlyFlag, "-trimpath", "-ldflags", "-s -w", "-o", outputPath, wrapperPath)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	return nil
}

func goPackageField(root, buildTarget, field, goos, goarch string) (string, error) {
	cmd := exec.Command("go", "list", goReadonlyFlag, "-f", field, buildTarget)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if isMissingGoToolError(err) {
			return "", fmt.Errorf("%w: %v", ErrGoToolUnavailable, err)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(string(out)), nil
}

func isMissingGoToolError(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, `exec: "go": executable file not found`) ||
		strings.Contains(msg, "executable file not found in $PATH")
}

func goProviderSourceExists(root string) bool {
	return goSourceExists(filepath.Join(root, "provider"))
}

func goComponentSourceExists(root string) bool {
	return goSourceExists(root)
}

func goSourceExists(root string) bool {
	if _, err := os.Stat(root); err != nil {
		return false
	}

	stop := errors.New("found go source")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") && !strings.HasSuffix(d.Name(), "_test.go") {
			return stop
		}
		return nil
	})
	return errors.Is(err, stop)
}

func isMissingGoPackageError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "directory not found") ||
		strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "go.mod file not found") ||
		strings.Contains(msg, "cannot find main module") ||
		strings.Contains(msg, "does not contain main module or its selected dependencies") ||
		strings.Contains(msg, "no Go files") ||
		strings.Contains(msg, "build constraints exclude all Go files") ||
		strings.Contains(msg, "matched no packages")
}

func sourcePluginName(root string) string {
	fallback := filepath.Base(filepath.Clean(root))
	manifestPath, err := FindManifestFile(root)
	if err != nil {
		return slugPluginName(fallback)
	}
	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		return slugPluginName(fallback)
	}
	return sourcePluginManifestName(manifest, fallback)
}

func sourcePluginManifestName(manifest *providermanifestv1.Manifest, fallback string) string {
	if manifest != nil {
		if manifest.Source != "" {
			parts := strings.Split(strings.TrimSpace(manifest.Source), "/")
			if last := parts[len(parts)-1]; last != "" {
				return slugPluginName(last)
			}
		}
		if manifest.DisplayName != "" {
			return slugPluginName(manifest.DisplayName)
		}
	}
	return slugPluginName(fallback)
}

func slugPluginName(value string) string {
	cleaned := goProviderNameSlugPattern.ReplaceAllString(strings.TrimSpace(value), "-")
	cleaned = strings.Trim(cleaned, "-")
	if cleaned == "" {
		return "plugin"
	}
	return cleaned
}

func validateSourceComponentKind(kind string) error {
	switch kind {
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindFileAPI, providermanifestv1.KindSecrets:
		return nil
	default:
		return fmt.Errorf("unsupported source component kind %q", kind)
	}
}

func componentServeCall(kind string) (string, error) {
	switch kind {
	case providermanifestv1.KindAuth:
		return "gestalt.ServeAuthProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindIndexedDB:
		return "gestalt.ServeIndexedDBProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindFileAPI:
		return "gestalt.ServeFileAPIProvider(ctx, providerpkg.New())", nil
	case providermanifestv1.KindSecrets:
		return "gestalt.ServeSecretsProvider(ctx, providerpkg.New())", nil
	default:
		return "", fmt.Errorf("unsupported source component kind %q", kind)
	}
}
