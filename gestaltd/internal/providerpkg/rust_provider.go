package providerpkg

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const (
	rustProjectFile       = "Cargo.toml"
	rustWrapperBinaryName = "gestalt-provider-wrapper"
)

var (
	ErrNoRustProviderPackage = errors.New("no Rust provider package found")
	ErrRustToolUnavailable   = errors.New("cargo tool unavailable")
)

type rustProviderTarget struct {
	PackageName string
}

type rustWrapperData struct {
	PluginNameLiteral  string
	RootPathLiteral    string
	PackageNameLiteral string
	ServeFunction      string
	SupportsCatalog    bool
}

var rustWrapperCargoTemplate = template.Must(template.New("rust-wrapper-cargo").Parse(`[package]
name = "gestalt-provider-wrapper"
version = "0.0.0"
edition = "2024"

[dependencies]
provider_plugin = { package = {{.PackageNameLiteral}}, path = {{.RootPathLiteral}} }
`))

var rustWrapperMainTemplate = template.Must(template.New("rust-wrapper-main").Parse(`use std::error::Error;

const PLUGIN_NAME: &str = {{.PluginNameLiteral}};

fn main() -> Result<(), Box<dyn Error + Send + Sync>> {
{{- if .SupportsCatalog }}
    if let Ok(path) = std::env::var("GESTALT_PLUGIN_WRITE_CATALOG") {
        provider_plugin::__gestalt_write_catalog(PLUGIN_NAME, &path)?;
    } else {
        provider_plugin::{{.ServeFunction}}(PLUGIN_NAME)?;
    }
{{- else }}
    provider_plugin::{{.ServeFunction}}(PLUGIN_NAME)?;
{{- end }}
    Ok(())
}
`))

func detectRustProviderPackage(root string) (*rustProviderTarget, error) {
	return detectRustPackage(root)
}

func detectRustPackage(root string) (*rustProviderTarget, error) {
	projectPath := filepath.Join(root, rustProjectFile)
	if _, err := os.Stat(projectPath); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoRustProviderPackage
		}
		return nil, fmt.Errorf("stat %s: %w", rustProjectFile, err)
	}

	data, err := os.ReadFile(projectPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rustProjectFile, err)
	}
	target, err := rustProjectPackageTarget(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rustProjectFile, err)
	}
	if target == nil {
		return nil, ErrNoRustProviderPackage
	}
	return target, nil
}

func rustProviderExecutionCommand(root, goos, goarch string) (string, []string, func(), error) {
	pluginName := sourcePluginName(root)
	command, cleanup, err := BuildRustProviderTempBinary(root, pluginName, goos, goarch)
	if err != nil {
		return "", nil, nil, err
	}
	return command, nil, cleanup, nil
}

func rustComponentExecutionCommand(root, kind, goos, goarch string) (string, []string, func(), error) {
	command, cleanup, err := BuildRustComponentTempBinary(root, kind, goos, goarch)
	if err != nil {
		return "", nil, nil, err
	}
	return command, nil, cleanup, nil
}

func BuildRustProviderTempBinary(root, pluginName, goos, goarch string) (string, func(), error) {
	return buildRustTempBinary(root, pluginName, providermanifestv1.KindPlugin, goos, goarch)
}

func BuildRustComponentTempBinary(root, kind, goos, goarch string) (string, func(), error) {
	if err := validateRustComponentKind(kind); err != nil {
		return "", nil, err
	}
	return buildRustTempBinary(root, sourcePluginName(root), kind, goos, goarch)
}

func buildRustTempBinary(root, pluginName, kind, goos, goarch string) (string, func(), error) {
	return buildGoTempBinary("gestalt-rust-provider-bin-*", rustBinaryName(kind), goos, func(outputPath string) error {
		_, err := buildRustBinary(root, outputPath, pluginName, kind, goos, goarch)
		return err
	})
}

func ValidateRustProviderRelease(root, goos, goarch string) error {
	if _, err := detectRustPackage(root); err != nil {
		return err
	}
	if _, err := rustTargetTriple(goos, goarch); err != nil {
		return err
	}
	return ensureCargoAvailable()
}

func ValidateRustComponentRelease(root, kind, goos, goarch string) error {
	if err := validateRustComponentKind(kind); err != nil {
		return err
	}
	if _, err := detectRustPackage(root); err != nil {
		return err
	}
	if _, err := rustTargetTriple(goos, goarch); err != nil {
		return err
	}
	return ensureCargoAvailable()
}

func BuildRustProviderBinary(root, outputPath, pluginName, goos, goarch string) (string, error) {
	return buildRustBinary(root, outputPath, pluginName, providermanifestv1.KindPlugin, goos, goarch)
}

func BuildRustComponentBinary(root, outputPath, kind, goos, goarch string) (string, error) {
	if err := validateRustComponentKind(kind); err != nil {
		return "", err
	}
	return buildRustBinary(root, outputPath, sourcePluginName(root), kind, goos, goarch)
}

func validateRustComponentKind(kind string) error {
	if err := validateSourceComponentKind(kind); err != nil {
		return err
	}
	if kind == providermanifestv1.KindAuthorization {
		return fmt.Errorf("unsupported Rust provider kind %q", kind)
	}
	return nil
}

func buildRustBinary(root, outputPath, pluginName, kind, goos, goarch string) (string, error) {
	target, err := detectRustPackage(root)
	if err != nil {
		return "", err
	}
	targetTriple, err := rustTargetTriple(goos, goarch)
	if err != nil {
		return "", err
	}
	if err := ensureCargoAvailable(); err != nil {
		return "", err
	}

	wrapperDir, cleanup, err := newRustWrapperProject(root, target.PackageName, pluginName, kind)
	if err != nil {
		return "", err
	}
	defer cleanup()

	targetDir := filepath.Join(wrapperDir, "target")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	cmd := exec.Command("cargo", "build",
		"--manifest-path", filepath.Join(wrapperDir, rustProjectFile),
		"--release",
		"--target", targetTriple,
		"--target-dir", targetDir,
	)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cargo build: %w", err)
	}

	builtBinaryPath := filepath.Join(targetDir, targetTriple, "release", rustWrapperBinaryName)
	if goos == "windows" {
		builtBinaryPath += ".exe"
	}
	if err := copyRustBuiltBinary(builtBinaryPath, outputPath); err != nil {
		return "", err
	}

	return "", nil
}

func rustProjectPackageTarget(data []byte) (*rustProviderTarget, error) {
	var (
		inPackageSection  bool
		sawPackageSection bool
		packageName       string
	)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(stripTOMLComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			inPackageSection = section == "package"
			if inPackageSection {
				sawPackageSection = true
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if inPackageSection && key == "name" {
			parsed, err := parseTOMLString(value)
			if err != nil {
				return nil, fmt.Errorf("package.name: %w", err)
			}
			packageName = strings.TrimSpace(parsed)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !sawPackageSection {
		return nil, nil
	}
	if packageName == "" {
		return nil, fmt.Errorf("package.name is required")
	}
	return &rustProviderTarget{PackageName: packageName}, nil
}

func ensureCargoAvailable() error {
	if _, err := exec.LookPath("cargo"); err != nil {
		return fmt.Errorf("%w: %v", ErrRustToolUnavailable, err)
	}
	return nil
}

func rustTargetTriple(goos, goarch string) (string, error) {
	switch goos {
	case "darwin":
		switch goarch {
		case "amd64":
			return "x86_64-apple-darwin", nil
		case "arm64":
			return "aarch64-apple-darwin", nil
		}
	case "linux":
		switch goarch {
		case "amd64":
			return "x86_64-unknown-linux-musl", nil
		case "arm64":
			return "aarch64-unknown-linux-musl", nil
		}
	case "windows":
		if goarch == "amd64" {
			return "x86_64-pc-windows-gnu", nil
		}
	}
	return "", fmt.Errorf("unsupported Rust target platform %s/%s", goos, goarch)
}

func rustServeFunction(kind string) (string, bool, error) {
	kind = providermanifestv1.NormalizeKind(kind)
	switch kind {
	case providermanifestv1.KindPlugin:
		return "__gestalt_serve", true, nil
	case providermanifestv1.KindAuthentication:
		return "__gestalt_serve_authentication", false, nil
	case providermanifestv1.KindCache:
		return "__gestalt_serve_cache", false, nil
	case providermanifestv1.KindIndexedDB:
		return "__gestalt_serve_indexeddb", false, nil
	case providermanifestv1.KindS3:
		return "__gestalt_serve_s3", false, nil
	case providermanifestv1.KindWorkflow:
		return "__gestalt_serve_workflow", false, nil
	case providermanifestv1.KindAgent:
		return "__gestalt_serve_agent", false, nil
	case providermanifestv1.KindSecrets:
		return "__gestalt_serve_secrets", false, nil
	default:
		return "", false, fmt.Errorf("unsupported Rust provider kind %q", kind)
	}
}

func rustBinaryName(kind string) string {
	kind = providermanifestv1.NormalizeKind(kind)
	switch kind {
	case providermanifestv1.KindAuthentication, providermanifestv1.KindCache, providermanifestv1.KindIndexedDB, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets:
		return kind
	default:
		return "provider"
	}
}

func newRustWrapperProject(root, packageName, pluginName, kind string) (string, func(), error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve Rust package root: %w", err)
	}
	serveFunction, supportsCatalog, err := rustServeFunction(kind)
	if err != nil {
		return "", nil, err
	}

	wrapperDir, err := os.MkdirTemp("", "gestalt-rust-wrapper-*")
	if err != nil {
		return "", nil, fmt.Errorf("create Rust wrapper directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(wrapperDir) }

	srcDir := filepath.Join(wrapperDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("create Rust wrapper src directory: %w", err)
	}

	data := rustWrapperData{
		PluginNameLiteral:  strconv.Quote(pluginName),
		RootPathLiteral:    strconv.Quote(absRoot),
		PackageNameLiteral: strconv.Quote(packageName),
		ServeFunction:      serveFunction,
		SupportsCatalog:    supportsCatalog,
	}
	if err := writeRustWrapperFile(filepath.Join(wrapperDir, rustProjectFile), rustWrapperCargoTemplate, data); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := writeRustWrapperFile(filepath.Join(srcDir, "main.rs"), rustWrapperMainTemplate, data); err != nil {
		cleanup()
		return "", nil, err
	}
	return wrapperDir, cleanup, nil
}

func writeRustWrapperFile(path string, tmpl *template.Template, data rustWrapperData) error {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func copyRustBuiltBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open built Rust binary %q: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat built Rust binary %q: %w", src, err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("create Rust provider binary %q: %w", dst, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy Rust provider binary: %w", err)
	}
	return out.Close()
}
