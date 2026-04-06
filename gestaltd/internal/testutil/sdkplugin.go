package testutil

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func SDKGoModulePath(t *testing.T) string {
	t.Helper()
	root, ok := repoRoot()
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(root, "sdk", "go")
}

func ExampleProviderPluginPath(t *testing.T) string {
	t.Helper()
	root, ok := repoRoot()
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(root, "examples", "plugins", "provider-go")
}

func CopyExampleProviderPlugin(t *testing.T, dst string) {
	t.Helper()

	src := ExampleProviderPluginPath(t)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dst, err)
	}
	if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
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
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}); err != nil {
		t.Fatalf("copy example provider plugin: %v", err)
	}

	goModPath := filepath.Join(dst, "go.mod")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read %s: %v", goModPath, err)
	}
	updated := strings.ReplaceAll(string(goMod), "../../../sdk/go", SDKGoModulePath(t))
	if err := os.WriteFile(goModPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write %s: %v", goModPath, err)
	}
}

func repoRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), true
}

func GeneratedProviderPackageSource() string {
	return `package provider

import (
	"context"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) generatedOp(context.Context, struct{}, gestalt.Request) (gestalt.Response[map[string]any], error) {
	return gestalt.OK(map[string]any{}), nil
}

var Router = gestalt.MustRouter(
	"generated",
	gestalt.Register(gestalt.Operation[struct{}, map[string]any]{ID: "generated_op"}, (*Provider).generatedOp),
)
`
}

func GeneratedProviderModuleSource(t *testing.T, module string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ExampleProviderPluginPath(t), "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(example go.mod): %v", err)
	}
	source := rewriteModuleLine(t, string(data), "module ", "module "+module)
	return rewriteModuleLine(
		t,
		source,
		"replace github.com/valon-technologies/gestalt/sdk/go => ",
		"replace github.com/valon-technologies/gestalt/sdk/go => "+SDKGoModulePath(t),
	)
}

func GeneratedProviderModuleSum(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ExampleProviderPluginPath(t), "go.sum"))
	if err != nil {
		t.Fatalf("ReadFile(example go.sum): %v", err)
	}
	return data
}

func rewriteModuleLine(t *testing.T, source, prefix, replacement string) string {
	t.Helper()
	lines := strings.Split(source, "\n")
	for i := range lines {
		if strings.HasPrefix(lines[i], prefix) {
			lines[i] = replacement
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("missing line prefix %q", prefix)
	return ""
}
