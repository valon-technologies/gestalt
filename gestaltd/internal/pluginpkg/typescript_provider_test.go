package pluginpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

const (
	typeScriptTestPluginModuleTarget    = "./provider.ts#plugin"
	typeScriptTestPluginTarget          = "plugin:./provider.ts#plugin"
	typeScriptTestAuthModuleTarget      = "./auth.ts#auth"
	typeScriptTestAuthTarget            = "auth:./auth.ts#auth"
	typeScriptTestDatastoreModuleTarget = "./datastore.ts#datastore"
	typeScriptTestDatastoreTarget       = "indexeddb:./datastore.ts#datastore"
)

func TestSplitTypeScriptProviderTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		target     string
		wantModule string
		wantExport string
		wantErr    string
	}{
		{
			name:       "module only",
			target:     "./provider.ts",
			wantModule: "./provider.ts",
		},
		{
			name:       "module and export",
			target:     typeScriptTestPluginModuleTarget,
			wantModule: "./provider.ts",
			wantExport: "plugin",
		},
		{
			name:    "missing relative prefix",
			target:  "provider.ts#plugin",
			wantErr: "module path must be relative",
		},
		{
			name:    "escapes root",
			target:  "../provider.ts#plugin",
			wantErr: "module path must stay within the plugin root",
		},
		{
			name:    "invalid extension",
			target:  "./provider",
			wantErr: "module path must end in a TypeScript or JavaScript file extension",
		},
		{
			name:    "invalid export",
			target:  "./provider.ts#bad-export",
			wantErr: "export must be a JavaScript identifier",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			module, exportName, err := SplitTypeScriptProviderTarget(tt.target)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("SplitTypeScriptProviderTarget(%q) error = nil, want %q", tt.target, tt.wantErr)
				}
				if !containsString(err.Error(), tt.wantErr) {
					t.Fatalf("SplitTypeScriptProviderTarget(%q) error = %q, want %q", tt.target, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitTypeScriptProviderTarget(%q): %v", tt.target, err)
			}
			if module != tt.wantModule || exportName != tt.wantExport {
				t.Fatalf("SplitTypeScriptProviderTarget(%q) = (%q, %q), want (%q, %q)", tt.target, module, exportName, tt.wantModule, tt.wantExport)
			}
		})
	}
}

func TestDetectTypeScriptProviderTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{
			name:   "plugin prefix",
			target: typeScriptTestPluginTarget,
			want:   typeScriptTestPluginTarget,
		},
		{
			name:   "legacy integration prefix",
			target: "integration:./provider.ts#plugin",
			want:   typeScriptTestPluginTarget,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			mustWriteTypeScriptProviderPackage(t, root, tt.target)

			got, err := DetectTypeScriptProviderTarget(root)
			if err != nil {
				t.Fatalf("DetectTypeScriptProviderTarget: %v", err)
			}
			if got != tt.want {
				t.Fatalf("target = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectTypeScriptProviderTarget_InvalidTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, typeScriptProjectFile), []byte(`{"name":"ts-release","gestalt":{"plugin":"provider.ts"}}`), 0o644)

	_, err := DetectTypeScriptProviderTarget(root)
	if err == nil {
		t.Fatal("expected invalid TypeScript provider target error")
	}
	if want := "package.json gestalt.plugin"; !containsString(err.Error(), want) {
		t.Fatalf("error = %q, want mention of %q", err, want)
	}
}

func TestDetectTypeScriptProviderTarget_RejectsEmptyKindPrefix(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteTypeScriptPackageConfig(t, root, map[string]string{
		typeScriptProviderKey: ":./provider.ts#plugin",
	})
	mustWriteTypeScriptTargetModule(t, root, typeScriptTestPluginTarget)

	_, err := DetectTypeScriptProviderTarget(root)
	if err == nil {
		t.Fatal("expected invalid TypeScript provider target error")
	}
	if want := "package.json gestalt.provider"; !containsString(err.Error(), want) {
		t.Fatalf("error = %q, want mention of %q", err, want)
	}
}

func TestHasSourceProviderPackage_TypeScript(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)

	ok, err := HasSourceProviderPackage(root)
	if err != nil {
		t.Fatalf("HasSourceProviderPackage: %v", err)
	}
	if !ok {
		t.Fatal("HasSourceProviderPackage = false, want true")
	}
}

func TestDetectTypeScriptComponentTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   string
		target string
	}{
		{
			name:   "auth",
			kind:   pluginmanifestv1.KindAuth,
			target: typeScriptTestAuthTarget,
		},
		{
			name:   "indexeddb",
			kind:   pluginmanifestv1.KindIndexedDB,
			target: typeScriptTestDatastoreTarget,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			mustWriteTypeScriptComponentPackage(t, root, tt.kind, tt.target)

			got, err := DetectTypeScriptComponentTarget(root, tt.kind)
			if err != nil {
				t.Fatalf("DetectTypeScriptComponentTarget(%q): %v", tt.kind, err)
			}
			if got != tt.target {
				t.Fatalf("target = %q, want %q", got, tt.target)
			}
		})
	}
}

func TestDetectTypeScriptComponentTarget_InvalidTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteTypeScriptPackageConfig(t, root, map[string]string{
		typeScriptAuthKey: "auth.ts",
	})

	_, err := DetectTypeScriptComponentTarget(root, pluginmanifestv1.KindAuth)
	if err == nil {
		t.Fatal("expected invalid TypeScript auth target error")
	}
	if want := "package.json gestalt.auth"; !containsString(err.Error(), want) {
		t.Fatalf("error = %q, want mention of %q", err, want)
	}
}

func TestHasSourceComponentPackage_TypeScript(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteTypeScriptComponentPackage(t, root, pluginmanifestv1.KindAuth, typeScriptTestAuthTarget)

	ok, err := HasSourceComponentPackage(root, pluginmanifestv1.KindAuth)
	if err != nil {
		t.Fatalf("HasSourceComponentPackage(auth): %v", err)
	}
	if !ok {
		t.Fatal("HasSourceComponentPackage(auth) = false, want true")
	}

	ok, err = HasSourceComponentPackage(root, pluginmanifestv1.KindIndexedDB)
	if err != nil {
		t.Fatalf("HasSourceComponentPackage(datastore): %v", err)
	}
	if ok {
		t.Fatal("HasSourceComponentPackage(datastore) = true, want false")
	}
}

func TestDetectBunExecutable_UsesEnvironmentOverride(t *testing.T) {
	root := t.TempDir()
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH)
	t.Setenv(typeScriptBunEnvVar, bunPath)
	t.Setenv("PATH", "")
	t.Setenv("HOME", filepath.Join(root, "home"))

	got, err := DetectBunExecutable()
	if err != nil {
		t.Fatalf("DetectBunExecutable: %v", err)
	}
	if got != bunPath {
		t.Fatalf("bun path = %q, want %q", got, bunPath)
	}
}

func TestSourceProviderExecutionCommand_TypeScript(t *testing.T) {
	root := t.TempDir()
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH)
	t.Setenv(typeScriptBunEnvVar, bunPath)

	command, args, cleanup, err := SourceProviderExecutionCommand(root, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("SourceProviderExecutionCommand: %v", err)
	}
	if cleanup != nil {
		t.Fatal("cleanup should be nil for TypeScript source providers")
	}
	if command != bunPath {
		t.Fatalf("command = %q, want %q", command, bunPath)
	}
	if len(args) != 7 {
		t.Fatalf("args len = %d, want 7 (%v)", len(args), args)
	}
	if args[0] != "--cwd" || args[1] != root {
		t.Fatalf("args prefix = %v, want [--cwd %s ...]", args[:2], root)
	}
	if args[2] != "run" {
		t.Fatalf("args[2] = %q, want run", args[2])
	}
	if args[4] != "--" {
		t.Fatalf("args[4] = %q, want --", args[4])
	}
	if args[5] != root || args[6] != typeScriptTestPluginTarget {
		t.Fatalf("args tail = %v, want [%s %s]", args[5:], root, typeScriptTestPluginTarget)
	}

	entry := filepath.Base(args[3])
	switch entry {
	case typeScriptRuntimeBin, "runtime.ts":
	default:
		t.Fatalf("runtime entry = %q, want %q or local runtime.ts path", args[3], typeScriptRuntimeBin)
	}
}

func TestSourceComponentExecutionCommand_TypeScript(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		target string
	}{
		{
			name:   "auth",
			kind:   pluginmanifestv1.KindAuth,
			target: typeScriptTestAuthTarget,
		},
		{
			name:   "indexeddb",
			kind:   pluginmanifestv1.KindIndexedDB,
			target: typeScriptTestDatastoreTarget,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mustWriteTypeScriptComponentPackage(t, root, tt.kind, tt.target)
			bunPath := writeFakeTypeScriptBun(t, root, "ts-release", tt.target, runtime.GOOS, runtime.GOARCH)
			t.Setenv(typeScriptBunEnvVar, bunPath)

			command, args, cleanup, err := SourceComponentExecutionCommand(root, tt.kind, runtime.GOOS, runtime.GOARCH)
			if err != nil {
				t.Fatalf("SourceComponentExecutionCommand(%q): %v", tt.kind, err)
			}
			if cleanup != nil {
				t.Fatal("cleanup should be nil for TypeScript source components")
			}
			if command != bunPath {
				t.Fatalf("command = %q, want %q", command, bunPath)
			}
			if len(args) != 7 {
				t.Fatalf("args len = %d, want 7 (%v)", len(args), args)
			}
			if args[5] != root || args[6] != tt.target {
				t.Fatalf("args tail = %v, want [%s %s]", args[5:], root, tt.target)
			}
		})
	}
}

func TestPrepareSourceManifest_GeneratesTypeScriptStaticCatalog(t *testing.T) {
	root := t.TempDir()
	manifestPath := mustWriteTypeScriptSourceManifest(t, root, "ts-release")
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH)
	t.Setenv(typeScriptBunEnvVar, bunPath)

	_, manifest, err := PrepareSourceManifest(manifestPath)
	if err != nil {
		t.Fatalf("PrepareSourceManifest: %v", err)
	}
	if manifest == nil || manifest.Spec == nil {
		t.Fatalf("manifest = %+v, want provider metadata", manifest)
	}

	catalogPath := filepath.Join(root, StaticCatalogFile)
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", catalogPath, err)
	}
	if !containsString(string(data), "name: ts-release") {
		t.Fatalf("catalog = %q, want provider name", string(data))
	}
}

func TestValidateSourceProviderRelease_TypeScript(t *testing.T) {
	root := t.TempDir()
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH)
	t.Setenv(typeScriptBunEnvVar, bunPath)

	if err := ValidateSourceProviderRelease(root, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("ValidateSourceProviderRelease: %v", err)
	}
}

func TestValidateSourceComponentRelease_TypeScript(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		target string
	}{
		{
			name:   "auth",
			kind:   pluginmanifestv1.KindAuth,
			target: typeScriptTestAuthTarget,
		},
		{
			name:   "indexeddb",
			kind:   pluginmanifestv1.KindIndexedDB,
			target: typeScriptTestDatastoreTarget,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mustWriteTypeScriptComponentPackage(t, root, tt.kind, tt.target)
			bunPath := writeFakeTypeScriptBun(t, root, "ts-release", tt.target, runtime.GOOS, runtime.GOARCH)
			t.Setenv(typeScriptBunEnvVar, bunPath)

			if err := ValidateSourceComponentRelease(root, tt.kind, runtime.GOOS, runtime.GOARCH); err != nil {
				t.Fatalf("ValidateSourceComponentRelease(%q): %v", tt.kind, err)
			}
		})
	}
}

func TestBuildSourceProviderReleaseBinary_TypeScript(t *testing.T) {
	root := t.TempDir()
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH)
	t.Setenv(typeScriptBunEnvVar, bunPath)

	outputPath := filepath.Join(t.TempDir(), "gestalt-plugin-ts-release")
	libc, err := BuildSourceProviderReleaseBinary(root, outputPath, "ts-release", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("BuildSourceProviderReleaseBinary: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", outputPath, err)
	}
	if !containsString(string(data), "fake ts release binary") {
		t.Fatalf("binary contents = %q, want fake build marker", string(data))
	}

	wantLibC := ""
	if runtime.GOOS == "linux" {
		wantLibC = ""
	}
	if libc != wantLibC {
		t.Fatalf("libc = %q, want %q", libc, wantLibC)
	}
}

func TestBuildSourceComponentReleaseBinary_TypeScript(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		target     string
		pluginName string
	}{
		{
			name:       "auth",
			kind:       pluginmanifestv1.KindAuth,
			target:     typeScriptTestAuthTarget,
			pluginName: "ts-auth-release",
		},
		{
			name:       "indexeddb",
			kind:       pluginmanifestv1.KindIndexedDB,
			target:     typeScriptTestDatastoreTarget,
			pluginName: "ts-datastore-release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mustWriteTypeScriptComponentPackage(t, root, tt.kind, tt.target)
			mustWriteTypeScriptSourceComponentManifest(t, root, tt.pluginName, tt.kind)
			bunPath := writeFakeTypeScriptBun(t, root, tt.pluginName, tt.target, runtime.GOOS, runtime.GOARCH)
			t.Setenv(typeScriptBunEnvVar, bunPath)

			outputPath := filepath.Join(t.TempDir(), "gestalt-plugin-"+tt.pluginName)
			libc, err := BuildSourceComponentReleaseBinary(root, outputPath, tt.kind, runtime.GOOS, runtime.GOARCH)
			if err != nil {
				t.Fatalf("BuildSourceComponentReleaseBinary(%q): %v", tt.kind, err)
			}

			data, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("ReadFile(%q): %v", outputPath, err)
			}
			if !containsString(string(data), "fake ts release binary") {
				t.Fatalf("binary contents = %q, want fake build marker", string(data))
			}

			wantLibC := ""
			if runtime.GOOS == "linux" {
				wantLibC = ""
			}
			if libc != wantLibC {
				t.Fatalf("libc = %q, want %q", libc, wantLibC)
			}
		})
	}
}

func mustWriteTypeScriptProviderPackage(t *testing.T, root, target string) {
	t.Helper()

	mustWriteTypeScriptPackageConfig(t, root, map[string]string{
		typeScriptProviderKey: target,
	})
	mustWriteTypeScriptTargetModule(t, root, target)
}

func mustWriteTypeScriptComponentPackage(t *testing.T, root, kind, target string) {
	t.Helper()

	providerKind, err := typeScriptComponentKind(kind)
	if err != nil {
		t.Fatalf("typeScriptComponentKind(%q): %v", kind, err)
	}
	mustWriteTypeScriptPackageConfig(t, root, map[string]string{
		typeScriptProviderKey: providerKind + ":" + runtimeTargetModulePath(t, target),
	})
	mustWriteTypeScriptTargetModule(t, root, target)
}

func mustWriteTypeScriptPackageConfig(t *testing.T, root string, gestalt map[string]string) {
	t.Helper()

	var fields []string
	for _, key := range []string{typeScriptProviderKey, typeScriptPluginKey, typeScriptAuthKey, typeScriptIndexedDBKey} {
		value, ok := gestalt[key]
		if !ok {
			continue
		}
		fields = append(fields, fmt.Sprintf("    %q: %q", key, value))
	}
	mustWriteFile(t, filepath.Join(root, typeScriptProjectFile), []byte(fmt.Sprintf(`{
  "name": "ts-release",
  "version": "0.0.1",
  "gestalt": {
%s
  }
}
`, strings.Join(fields, ",\n"))), 0o644)
}

func mustWriteTypeScriptTargetModule(t *testing.T, root, target string) {
	t.Helper()

	modulePath, exportName, err := SplitTypeScriptProviderTarget(runtimeTargetModulePath(t, target))
	if err != nil {
		t.Fatalf("SplitTypeScriptProviderTarget(%q): %v", target, err)
	}
	modulePath = strings.TrimPrefix(filepath.ToSlash(modulePath), "./")
	if exportName == "" {
		exportName = "plugin"
	}
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(modulePath)), []byte("export const "+exportName+" = {};\n"), 0o644)
}

func runtimeTargetModulePath(t *testing.T, target string) string {
	t.Helper()
	for _, prefix := range []string{"plugin:", "integration:", "auth:", "indexeddb:", "secrets:", "telemetry:"} {
		if strings.HasPrefix(target, prefix) {
			return strings.TrimPrefix(target, prefix)
		}
	}
	return target
}

func mustWriteTypeScriptSourceManifest(t *testing.T, root, pluginName string) string {
	t.Helper()

	data, err := EncodeSourceManifestFormat(&pluginmanifestv1.Manifest{
		Kind:    pluginmanifestv1.KindPlugin,
		Source:  "github.com/testowner/plugins/" + pluginName,
		Version: "0.0.1",
		Spec: &pluginmanifestv1.Spec{
			Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
		},
	}, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}

	path := filepath.Join(root, "manifest.yaml")
	mustWriteFile(t, path, data, 0o644)
	return path
}

func mustWriteTypeScriptSourceComponentManifest(t *testing.T, root, pluginName, kind string) string {
	t.Helper()

	manifest := &pluginmanifestv1.Manifest{
		Kind:    kind,
		Source:  "github.com/testowner/plugins/" + pluginName,
		Version: "0.0.1",
		Spec:    &pluginmanifestv1.Spec{},
	}

	data, err := EncodeSourceManifestFormat(manifest, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}

	path := filepath.Join(root, "manifest.yaml")
	mustWriteFile(t, path, data, 0o644)
	return path
}

func writeFakeTypeScriptBun(t *testing.T, root, pluginName, expectedTarget, expectedGOOS, expectedGOARCH string) string {
	t.Helper()

	path := filepath.Join(root, "bin", "bun")
	script := `#!/bin/sh
set -eu

if [ "$#" -lt 4 ] || [ "$1" != "--cwd" ]; then
  echo "unexpected fake bun args: $*" >&2
  exit 1
fi

cwd="$2"
shift 2

if [ "$1" != "run" ]; then
  echo "unexpected bun subcommand: $1" >&2
  exit 1
fi
shift

entry="$1"
shift

if [ "$#" -gt 0 ] && [ "$1" = "--" ]; then
  shift
fi

entry_base="${entry##*/}"

case "$entry_base" in
  gestalt-ts-runtime|runtime.ts)
    if [ "$#" -ne 2 ]; then
      echo "unexpected runtime args: $*" >&2
      exit 1
    fi
    root="$1"
    target="$2"
    if [ "$cwd" != "$root" ]; then
      echo "unexpected runtime cwd: $cwd != $root" >&2
      exit 1
    fi
    if [ "$target" != "` + expectedTarget + `" ]; then
      echo "unexpected runtime target: $target" >&2
      exit 1
    fi
    if [ -z "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
      echo "missing GESTALT_PLUGIN_WRITE_CATALOG" >&2
      exit 1
    fi
    cat > "$GESTALT_PLUGIN_WRITE_CATALOG" <<'EOF'
name: ` + pluginName + `
operations:
  - id: greet
    method: GET
EOF
    exit 0
    ;;
  gestalt-ts-build|build.ts)
    if [ "$#" -ne 6 ]; then
      echo "unexpected build args: $*" >&2
      exit 1
    fi
    source_dir="$1"
    target="$2"
    output="$3"
    name="$4"
    goos="$5"
    goarch="$6"
    if [ "$cwd" != "$source_dir" ]; then
      echo "unexpected build cwd: $cwd != $source_dir" >&2
      exit 1
    fi
    if [ "$target" != "` + expectedTarget + `" ]; then
      echo "unexpected build target: $target" >&2
      exit 1
    fi
    if [ "$name" != "` + pluginName + `" ]; then
      echo "unexpected plugin name: $name" >&2
      exit 1
    fi
    if [ "$goos" != "` + expectedGOOS + `" ] || [ "$goarch" != "` + expectedGOARCH + `" ]; then
      echo "unexpected target platform: $goos/$goarch" >&2
      exit 1
    fi
    output_dir="${output%/*}"
    if [ "$output_dir" = "$output" ]; then
      output_dir="."
    fi
    mkdir -p "$output_dir"
    printf '#!/bin/sh\n# fake ts release binary\nexit 0\n' > "$output"
    chmod +x "$output"
    exit 0
    ;;
esac

echo "unexpected fake bun entry: $entry ($*)" >&2
exit 1
`
	mustWriteFile(t, path, []byte(script), 0o755)
	return path
}
