package pluginpkg

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestDetectRustProviderPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRustProviderCargoToml(t, root)

	target, err := detectRustProviderPackage(root)
	if err != nil {
		t.Fatalf("detectRustProviderPackage: %v", err)
	}
	if target == nil {
		t.Fatal("target = nil")
	}
	if target.PackageName != "rust-provider" {
		t.Fatalf("PackageName = %q, want %q", target.PackageName, "rust-provider")
	}
}

func TestDetectRustProviderPackage_RequiresPackageName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, rustProjectFile), []byte("[package]\nversion = \"0.0.1\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", rustProjectFile, err)
	}

	_, err := detectRustProviderPackage(root)
	if err == nil || !strings.Contains(err.Error(), "package.name is required") {
		t.Fatalf("error = %v, want package.name failure", err)
	}
}

func TestRustProjectPackageTarget_WorkspaceManifestReturnsNil(t *testing.T) {
	t.Parallel()

	target, err := rustProjectPackageTarget([]byte("[workspace]\nmembers = [\"provider\"]\n"))
	if err != nil {
		t.Fatalf("rustProjectPackageTarget: %v", err)
	}
	if target != nil {
		t.Fatalf("target = %#v, want nil", target)
	}
}

func TestDetectRustProviderPackage_WorkspaceManifestReturnsNoPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRustWorkspaceCargoToml(t, root)

	_, err := detectRustProviderPackage(root)
	if !errors.Is(err, ErrNoRustProviderPackage) {
		t.Fatalf("error = %v, want %v", err, ErrNoRustProviderPackage)
	}
}

func TestBuildRustProviderBinary_UsesSynthesizedCargoWrapper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cargo test fixture is POSIX-only")
	}

	root := t.TempDir()
	writeRustProviderCargoToml(t, root)

	targetTriple, expectedLibC, err := rustTargetTriple(runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		t.Fatalf("rustTargetTriple(host): %v", err)
	}

	fakeCargoDir := t.TempDir()
	writeFakeCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeCargoConfig{
		ExpectedPluginName:   "rust-release",
		ExpectedTarget:       targetTriple,
		ExpectedServeExport:  "__gestalt_serve",
		ExpectedCatalogWrite: true,
		GeneratedCatalog:     "rust-release",
	})
	t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	outputPath := filepath.Join(t.TempDir(), "provider")
	builtLibC, err := BuildRustProviderBinary(root, outputPath, "rust-release", runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		t.Fatalf("BuildRustProviderBinary: %v", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("stat output binary: %v", err)
	}
	if builtLibC != expectedLibC {
		t.Fatalf("builtLibC = %q, want %q", builtLibC, expectedLibC)
	}
}

func TestBuildRustComponentBinary_UsesKindSpecificWrapper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cargo test fixture is POSIX-only")
	}

	tests := []struct {
		name                string
		kind                string
		expectedServeExport string
	}{
		{name: "auth", kind: "auth", expectedServeExport: "__gestalt_serve_auth"},
		{name: "indexeddb", kind: "indexeddb", expectedServeExport: "__gestalt_serve_indexeddb"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeRustProviderCargoToml(t, root)

			targetTriple, _, err := rustTargetTriple(runtime.GOOS, runtime.GOARCH, "")
			if err != nil {
				t.Fatalf("rustTargetTriple(host): %v", err)
			}

			fakeCargoDir := t.TempDir()
			writeFakeCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeCargoConfig{
				ExpectedPluginName:   sourcePluginName(root),
				ExpectedTarget:       targetTriple,
				ExpectedServeExport:  tt.expectedServeExport,
				ExpectedCatalogWrite: false,
			})
			t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

			outputPath := filepath.Join(t.TempDir(), tt.kind)
			if _, err := BuildRustComponentBinary(root, outputPath, tt.kind, runtime.GOOS, runtime.GOARCH, ""); err != nil {
				t.Fatalf("BuildRustComponentBinary(%s): %v", tt.kind, err)
			}
			if _, err := os.Stat(outputPath); err != nil {
				t.Fatalf("stat output binary: %v", err)
			}
		})
	}
}

func TestHasSourceComponentPackage_DetectsRustPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRustProviderCargoToml(t, root)

	ok, err := HasSourceComponentPackage(root, "auth")
	if err != nil {
		t.Fatalf("HasSourceComponentPackage(auth): %v", err)
	}
	if !ok {
		t.Fatal("HasSourceComponentPackage(auth) = false, want true")
	}
}

func TestNewRustWrapperProject_UsesAbsoluteDependencyPath(t *testing.T) { //nolint:paralleltest // changes process cwd
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX path assertions")
	}

	baseDir := t.TempDir()
	pluginDir := filepath.Join(baseDir, "provider-rust")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", pluginDir, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(baseDir); err != nil {
		t.Fatalf("Chdir(%s): %v", baseDir, err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	wrapperDir, cleanup, err := newRustWrapperProject("provider-rust", "provider-rust", "rust-release", "plugin")
	if err != nil {
		t.Fatalf("newRustWrapperProject: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(filepath.Join(wrapperDir, rustProjectFile))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", rustProjectFile, err)
	}
	match := regexp.MustCompile(`path = "([^"]+)"`).FindStringSubmatch(string(data))
	if len(match) != 2 {
		t.Fatalf("wrapper Cargo.toml = %q, want dependency path", data)
	}
	if !filepath.IsAbs(match[1]) {
		t.Fatalf("wrapper dependency path = %q, want absolute path", match[1])
	}
	if !strings.HasSuffix(match[1], string(filepath.Separator)+"provider-rust") {
		t.Fatalf("wrapper dependency path = %q, want provider-rust suffix", match[1])
	}
}

func TestBuildRustProviderBinary_UsesExplicitLinuxLibCTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cargo test fixture is POSIX-only")
	}

	root := t.TempDir()
	writeRustProviderCargoToml(t, root)

	fakeCargoDir := t.TempDir()
	writeFakeCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeCargoConfig{
		ExpectedPluginName:   "rust-release",
		ExpectedTarget:       "x86_64-unknown-linux-musl",
		ExpectedServeExport:  "__gestalt_serve",
		ExpectedCatalogWrite: true,
		GeneratedCatalog:     "rust-release",
	})
	t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	outputPath := filepath.Join(t.TempDir(), "provider")
	builtLibC, err := BuildRustProviderBinary(root, outputPath, "rust-release", "linux", "amd64", LinuxLibCMusl)
	if err != nil {
		t.Fatalf("BuildRustProviderBinary(linux/amd64/musl): %v", err)
	}
	if builtLibC != LinuxLibCMusl {
		t.Fatalf("builtLibC = %q, want %q", builtLibC, LinuxLibCMusl)
	}
}

func TestPrepareSourceManifest_GeneratesStaticCatalogForRustProvider(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cargo test fixture is POSIX-only")
	}

	root := t.TempDir()
	copyRustProviderFixture(t, root)

	targetTriple, _, err := rustTargetTriple(runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		t.Fatalf("rustTargetTriple(host): %v", err)
	}

	fakeCargoDir := t.TempDir()
	writeFakeCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeCargoConfig{
		ExpectedPluginName:   "provider-rust",
		ExpectedTarget:       targetTriple,
		ExpectedServeExport:  "__gestalt_serve",
		ExpectedCatalogWrite: true,
		GeneratedCatalog:     "provider-rust",
	})
	t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manifestPath := filepath.Join(root, "manifest.yaml")
	_, manifest, err := PrepareSourceManifest(manifestPath)
	if err != nil {
		t.Fatalf("PrepareSourceManifest: %v", err)
	}
	if manifest == nil || manifest.Plugin == nil {
		t.Fatalf("manifest = %#v, want plugin manifest", manifest)
	}

	catalogPath := filepath.Join(root, StaticCatalogFile)
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", catalogPath, err)
	}
	if !strings.Contains(string(data), "name: provider-rust") {
		t.Fatalf("catalog = %q, want provider name", data)
	}
	if !strings.Contains(string(data), "id: greet") {
		t.Fatalf("catalog = %q, want greet operation", data)
	}
}

func TestRustTargetTriple(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		goos     string
		goarch   string
		libc     string
		want     string
		wantLibC string
	}{
		{name: "darwin-amd64", goos: "darwin", goarch: "amd64", want: "x86_64-apple-darwin"},
		{name: "darwin-arm64", goos: "darwin", goarch: "arm64", want: "aarch64-apple-darwin"},
		{name: "windows-amd64", goos: "windows", goarch: "amd64", want: "x86_64-pc-windows-gnu"},
		{name: "linux-amd64-glibc", goos: "linux", goarch: "amd64", libc: LinuxLibCGLibC, want: "x86_64-unknown-linux-gnu", wantLibC: LinuxLibCGLibC},
		{name: "linux-amd64-musl", goos: "linux", goarch: "amd64", libc: LinuxLibCMusl, want: "x86_64-unknown-linux-musl", wantLibC: LinuxLibCMusl},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, gotLibC, err := rustTargetTriple(tt.goos, tt.goarch, tt.libc)
			if err != nil {
				t.Fatalf("rustTargetTriple: %v", err)
			}
			if got != tt.want {
				t.Fatalf("target = %q, want %q", got, tt.want)
			}
			if gotLibC != tt.wantLibC {
				t.Fatalf("libc = %q, want %q", gotLibC, tt.wantLibC)
			}
		})
	}
}

func writeRustProviderCargoToml(t *testing.T, root string) {
	t.Helper()

	content := `[package]
name = "rust-provider"
version = "0.0.1"
`
	if err := os.WriteFile(filepath.Join(root, rustProjectFile), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", rustProjectFile, err)
	}
}

func writeRustWorkspaceCargoToml(t *testing.T, root string) {
	t.Helper()

	content := `[workspace]
members = ["provider"]
`
	if err := os.WriteFile(filepath.Join(root, rustProjectFile), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", rustProjectFile, err)
	}
}

type fakeCargoConfig struct {
	ExpectedPluginName   string
	ExpectedTarget       string
	ExpectedServeExport  string
	ExpectedCatalogWrite bool
	GeneratedCatalog     string
}

func writeFakeCargo(t *testing.T, path string, cfg fakeCargoConfig) {
	t.Helper()

	script := `#!/bin/sh
set -eu

manifest=""
target=""
target_dir=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --manifest-path)
      manifest="$2"
      shift 2
      ;;
    --target)
      target="$2"
      shift 2
      ;;
    --target-dir)
      target_dir="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

if [ -z "$manifest" ] || [ -z "$target" ] || [ -z "$target_dir" ]; then
  echo "missing cargo wrapper args" >&2
  exit 1
fi

if [ "$target" != ` + shellSingleQuoted(cfg.ExpectedTarget) + ` ]; then
  echo "unexpected target triple: $target" >&2
  exit 1
fi

main_rs="$(dirname "$manifest")/src/main.rs"
if ! grep -q 'const PLUGIN_NAME: &str = ` + rustDoubleQuoted(cfg.ExpectedPluginName) + `;' "$main_rs"; then
  echo "missing plugin name in wrapper source" >&2
  exit 1
fi
if ! grep -Fq 'provider_plugin::` + cfg.ExpectedServeExport + `(PLUGIN_NAME)?' "$main_rs"; then
  echo "missing serve export in wrapper source" >&2
  exit 1
fi
` + fakeCargoCatalogCheck(cfg.ExpectedCatalogWrite) + `
if ! grep -Fq 'Ok(())' "$main_rs"; then
  echo "missing explicit Ok return in wrapper source" >&2
  exit 1
fi

binary="$target_dir/$target/release/` + rustWrapperBinaryName + `"
mkdir -p "$(dirname "$binary")"
cat > "$binary" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
  cat > "$GESTALT_PLUGIN_WRITE_CATALOG" <<'YAML'
name: ` + cfg.GeneratedCatalog + `
operations:
  - id: greet
    method: GET
YAML
fi
exit 0
EOF
chmod +x "$binary"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func copyRustProviderFixture(t *testing.T, dst string) {
	t.Helper()

	src := rustProviderFixturePath(t)
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
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
	}); err != nil {
		t.Fatalf("copy Rust provider fixture: %v", err)
	}
}

func fakeCargoCatalogCheck(expectCatalog bool) string {
	if expectCatalog {
		return `if ! grep -Fq 'provider_plugin::__gestalt_write_catalog(PLUGIN_NAME, &path)?' "$main_rs"; then
  echo "missing write-catalog export in wrapper source" >&2
  exit 1
fi`
	}
	return `if grep -Fq 'provider_plugin::__gestalt_write_catalog(PLUGIN_NAME, &path)?' "$main_rs"; then
  echo "unexpected write-catalog export in wrapper source" >&2
  exit 1
fi`
}

func rustProviderFixturePath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "testutil", "testdata", "provider-rust"))
}

func shellSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func rustDoubleQuoted(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
