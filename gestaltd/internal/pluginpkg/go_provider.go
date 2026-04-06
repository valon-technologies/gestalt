package pluginpkg

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
)

const goProviderPackageTarget = "."
const goReadonlyFlag = "-mod=readonly"

var ErrNoGoProviderPackage = errors.New("no Go provider package found")
var ErrGoToolUnavailable = errors.New("go tool unavailable")

//go:embed go_provider_wrapper.go.tmpl
var goProviderWrapperSource string

var goProviderWrapperTemplate = template.Must(template.New("go-provider-wrapper").Parse(goProviderWrapperSource))

func DetectGoProviderImportPath(root, goos, goarch string) (string, error) {
	importPath, err := goPackageField(root, goProviderPackageTarget, "{{.ImportPath}}", goos, goarch)
	if err != nil {
		if errors.Is(err, ErrGoToolUnavailable) {
			if !goProviderSourceExists(root) {
				return "", ErrNoGoProviderPackage
			}
			return "", err
		}
		if isMissingGoPackageError(err) {
			return "", ErrNoGoProviderPackage
		}
		return "", fmt.Errorf("%s: %w", goProviderPackageTarget, err)
	}
	return strings.TrimSpace(importPath), nil
}

func NewGoProviderWrapper(root, goos, goarch string) (string, func(), error) {
	importPath, err := DetectGoProviderImportPath(root, goos, goarch)
	if err != nil {
		return "", nil, err
	}

	file, err := os.CreateTemp("", "gestalt-go-provider-*.go")
	if err != nil {
		return "", nil, fmt.Errorf("create Go provider wrapper: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	defer func() {
		_ = file.Close()
	}()

	var buf bytes.Buffer
	if err := goProviderWrapperTemplate.Execute(&buf, struct{ ImportPath string }{ImportPath: importPath}); err != nil {
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

func GoProviderRunCommand(root string) (string, []string, func(), error) {
	wrapperPath, cleanup, err := NewGoProviderWrapper(root, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", nil, nil, err
	}
	return "go", []string{"-C", root, "run", goReadonlyFlag, wrapperPath}, cleanup, nil
}

func HasGoProviderPackage(root string) (bool, error) {
	_, err := DetectGoProviderImportPath(root, runtime.GOOS, runtime.GOARCH)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNoGoProviderPackage):
		return false, nil
	default:
		return false, err
	}
}

func BuildGoProviderTempBinary(root, goos, goarch string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "gestalt-go-provider-bin-*")
	if err != nil {
		return "", nil, fmt.Errorf("create Go provider temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }

	binaryName := "provider"
	if goos == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(tempDir, binaryName)
	if err := BuildGoProviderBinary(root, binaryPath, goos, goarch); err != nil {
		cleanup()
		return "", nil, err
	}
	return binaryPath, cleanup, nil
}

func BuildGoProviderBinary(root, outputPath, goos, goarch string) error {
	wrapperPath, cleanup, err := NewGoProviderWrapper(root, goos, goarch)
	if err != nil {
		return err
	}
	defer cleanup()

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
	providerDir := filepath.Join(root, "provider")
	if _, err := os.Stat(providerDir); err != nil {
		return false
	}

	stop := errors.New("found go provider source")
	err := filepath.WalkDir(providerDir, func(path string, d os.DirEntry, err error) error {
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
