package providerpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil/fakebun"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const (
	typeScriptTestPluginModuleTarget    = "./provider.ts#plugin"
	typeScriptTestPluginTarget          = "plugin:./provider.ts#plugin"
	typeScriptTestAuthModuleTarget      = "./auth.ts#auth"
	typeScriptTestAuthTarget            = "authentication:./auth.ts#auth"
	typeScriptTestCacheModuleTarget     = "./cache.ts#cache"
	typeScriptTestCacheTarget           = "cache:./cache.ts#cache"
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
	mustWriteFile(t, filepath.Join(root, typeScriptProjectFile), []byte(`{"name":"ts-release","gestalt":{"provider":"provider.ts"}}`), 0o644)

	_, err := DetectTypeScriptProviderTarget(root)
	if err == nil {
		t.Fatal("expected invalid TypeScript provider target error")
	}
	if want := "package.json gestalt.provider"; !containsString(err.Error(), want) {
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

func TestDetectTypeScriptProviderTarget_RejectsLegacyIntegrationPrefix(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteTypeScriptPackageConfig(t, root, map[string]string{
		typeScriptProviderKey: "integration:./provider.ts#plugin",
	})
	mustWriteTypeScriptTargetModule(t, root, typeScriptTestPluginTarget)

	_, err := DetectTypeScriptProviderTarget(root)
	if err == nil {
		t.Fatal("expected unsupported TypeScript provider kind error")
	}
	if want := `unsupported provider kind "integration"`; !containsString(err.Error(), want) {
		t.Fatalf("error = %q, want mention of %q", err, want)
	}
}

func TestDetectTypeScriptProviderTarget_ObjectConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, typeScriptProjectFile), []byte(`{
  "name": "ts-release",
  "version": "0.0.1",
  "gestalt": {
    "provider": {
      "kind": "plugin",
      "target": "./provider.ts#plugin"
    }
  }
}`), 0o644)
	mustWriteTypeScriptTargetModule(t, root, typeScriptTestPluginTarget)

	got, err := DetectTypeScriptProviderTarget(root)
	if err != nil {
		t.Fatalf("DetectTypeScriptProviderTarget: %v", err)
	}
	if got != typeScriptTestPluginTarget {
		t.Fatalf("target = %q, want %q", got, typeScriptTestPluginTarget)
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
			kind:   providermanifestv1.KindAuthentication,
			target: typeScriptTestAuthTarget,
		},
		{
			name:   "cache",
			kind:   providermanifestv1.KindCache,
			target: typeScriptTestCacheTarget,
		},
		{
			name:   "indexeddb",
			kind:   providermanifestv1.KindIndexedDB,
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
		typeScriptProviderKey: "auth:auth.ts",
	})

	_, err := DetectTypeScriptComponentTarget(root, providermanifestv1.KindAuthentication)
	if err == nil {
		t.Fatal("expected invalid TypeScript auth target error")
	}
	if want := "package.json gestalt.provider"; !containsString(err.Error(), want) {
		t.Fatalf("error = %q, want mention of %q", err, want)
	}
}

func TestHasSourceComponentPackage_TypeScript(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteTypeScriptComponentPackage(t, root, providermanifestv1.KindAuthentication, typeScriptTestAuthTarget)

	ok, err := HasSourceComponentPackage(root, providermanifestv1.KindAuthentication)
	if err != nil {
		t.Fatalf("HasSourceComponentPackage(auth): %v", err)
	}
	if !ok {
		t.Fatal("HasSourceComponentPackage(auth) = false, want true")
	}

	ok, err = HasSourceComponentPackage(root, providermanifestv1.KindCache)
	if err != nil {
		t.Fatalf("HasSourceComponentPackage(cache): %v", err)
	}
	if ok {
		t.Fatal("HasSourceComponentPackage(cache) = true, want false")
	}

	ok, err = HasSourceComponentPackage(root, providermanifestv1.KindIndexedDB)
	if err != nil {
		t.Fatalf("HasSourceComponentPackage(datastore): %v", err)
	}
	if ok {
		t.Fatal("HasSourceComponentPackage(datastore) = true, want false")
	}
}

func TestDetectBunExecutable_UsesEnvironmentOverride(t *testing.T) {
	root := t.TempDir()
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH, root)
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
	sdkDir := writeFakeTypeScriptSDKDir(t, t.TempDir())
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH, sdkDir)
	t.Setenv(typeScriptBunEnvVar, bunPath)
	t.Setenv(typeScriptSDKDirEnvVar, sdkDir)

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
	if len(args) != 6 {
		t.Fatalf("args len = %d, want 6 (%v)", len(args), args)
	}
	if args[0] != "--cwd" || args[1] != sdkDir {
		t.Fatalf("args prefix = %v, want [--cwd %s ...]", args[:2], sdkDir)
	}
	if args[3] != "--" {
		t.Fatalf("args[3] = %q, want --", args[3])
	}
	if args[4] != root || args[5] != typeScriptTestPluginTarget {
		t.Fatalf("args tail = %v, want [%s %s]", args[4:], root, typeScriptTestPluginTarget)
	}

	if got := filepath.Base(args[2]); got != "runtime.ts" {
		t.Fatalf("runtime entry = %q, want local runtime.ts path", args[2])
	}
}

func TestSourceProviderExecutionCommand_TypeScriptInstallsLocalSDKDeps(t *testing.T) {
	root := t.TempDir()
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)
	sdkDir := writeFakeTypeScriptSDKDir(t, t.TempDir())
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH, sdkDir)
	t.Setenv(typeScriptBunEnvVar, bunPath)
	t.Setenv(typeScriptSDKDirEnvVar, sdkDir)

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
	if _, err := os.Stat(filepath.Join(root, "node_modules", ".installed")); err != nil {
		t.Fatalf("expected bun install marker in provider node_modules: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sdkDir, "node_modules", ".installed")); err != nil {
		t.Fatalf("expected bun install marker in fake SDK node_modules: %v", err)
	}
	if got := filepath.Dir(args[2]); got != filepath.Join(sdkDir, "src") {
		t.Fatalf("runtime entry dir = %q, want %q", got, filepath.Join(sdkDir, "src"))
	}
}

func TestSourceProviderExecutionCommand_TypeScriptPackageRuntime(t *testing.T) {
	root := t.TempDir()
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)
	t.Setenv(typeScriptSDKDirEnvVar, filepath.Join(t.TempDir(), "missing-sdk"))

	bunPath := fakebun.NewExecutable(t, fakebun.Config{
		Install: &fakebun.InstallConfig{
			ExpectedCwd: root,
		},
		Runtime: &fakebun.RuntimeConfig{
			Mode:           fakebun.InvocationRun,
			ExpectedCwd:    root,
			ExpectedEntry:  typeScriptRuntimeBin,
			ExpectedRoot:   root,
			ExpectedTarget: typeScriptTestPluginTarget,
		},
	})
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
	if got := strings.Join(args, "\x00"); got != strings.Join([]string{
		"run",
		"--cwd",
		root,
		typeScriptRuntimeBin,
		"--",
		root,
		typeScriptTestPluginTarget,
	}, "\x00") {
		t.Fatalf("args = %v", args)
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules", ".installed")); err != nil {
		t.Fatalf("expected bun install marker in provider node_modules: %v", err)
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
			kind:   providermanifestv1.KindAuthentication,
			target: typeScriptTestAuthTarget,
		},
		{
			name:   "cache",
			kind:   providermanifestv1.KindCache,
			target: typeScriptTestCacheTarget,
		},
		{
			name:   "indexeddb",
			kind:   providermanifestv1.KindIndexedDB,
			target: typeScriptTestDatastoreTarget,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mustWriteTypeScriptComponentPackage(t, root, tt.kind, tt.target)
			sdkDir := writeFakeTypeScriptSDKDir(t, t.TempDir())
			bunPath := writeFakeTypeScriptBun(t, root, "ts-release", tt.target, runtime.GOOS, runtime.GOARCH, sdkDir)
			t.Setenv(typeScriptBunEnvVar, bunPath)
			t.Setenv(typeScriptSDKDirEnvVar, sdkDir)

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
			if len(args) != 6 {
				t.Fatalf("args len = %d, want 6 (%v)", len(args), args)
			}
			if got := filepath.Base(args[2]); got != "runtime.ts" {
				t.Fatalf("runtime entry = %q, want local runtime.ts path", args[2])
			}
			if args[4] != root || args[5] != tt.target {
				t.Fatalf("args tail = %v, want [%s %s]", args[4:], root, tt.target)
			}
		})
	}
}

func TestPrepareSourceManifest_GeneratesTypeScriptStaticCatalog(t *testing.T) {
	root := t.TempDir()
	manifestPath := mustWriteTypeScriptSourceManifest(t, root, "ts-release")
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)
	sdkDir := writeFakeTypeScriptSDKDir(t, t.TempDir())
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH, sdkDir)
	t.Setenv(typeScriptBunEnvVar, bunPath)
	t.Setenv(typeScriptSDKDirEnvVar, sdkDir)

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
	expectedCWD := localTypeScriptSDKPath()
	if expectedCWD == "" {
		expectedCWD = root
	}
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH, expectedCWD)
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
			kind:   providermanifestv1.KindAuthentication,
			target: typeScriptTestAuthTarget,
		},
		{
			name:   "cache",
			kind:   providermanifestv1.KindCache,
			target: typeScriptTestCacheTarget,
		},
		{
			name:   "indexeddb",
			kind:   providermanifestv1.KindIndexedDB,
			target: typeScriptTestDatastoreTarget,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mustWriteTypeScriptComponentPackage(t, root, tt.kind, tt.target)
			expectedCWD := localTypeScriptSDKPath()
			if expectedCWD == "" {
				expectedCWD = root
			}
			bunPath := writeFakeTypeScriptBun(t, root, "ts-release", tt.target, runtime.GOOS, runtime.GOARCH, expectedCWD)
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
	sdkDir := writeFakeTypeScriptSDKDir(t, t.TempDir())
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH, sdkDir)
	t.Setenv(typeScriptBunEnvVar, bunPath)
	t.Setenv(typeScriptSDKDirEnvVar, sdkDir)

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

func TestBuildSourceProviderReleaseBinary_TypeScriptInstallsLocalSDKDeps(t *testing.T) {
	root := t.TempDir()
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)
	sdkDir := writeFakeTypeScriptSDKDir(t, t.TempDir())
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH, sdkDir)
	t.Setenv(typeScriptBunEnvVar, bunPath)
	t.Setenv(typeScriptSDKDirEnvVar, sdkDir)

	outputPath := filepath.Join(t.TempDir(), "gestalt-plugin-ts-release")
	if _, err := BuildSourceProviderReleaseBinary(root, outputPath, "ts-release", runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildSourceProviderReleaseBinary: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules", ".installed")); err != nil {
		t.Fatalf("expected bun install marker in provider node_modules: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sdkDir, "node_modules", ".installed")); err != nil {
		t.Fatalf("expected bun install marker in fake SDK node_modules: %v", err)
	}
}

func TestBuildSourceProviderReleaseBinary_TypeScriptPackageBuild(t *testing.T) {
	root := t.TempDir()
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)
	t.Setenv(typeScriptSDKDirEnvVar, filepath.Join(t.TempDir(), "missing-sdk"))

	bunPath := fakebun.NewExecutable(t, fakebun.Config{
		Install: &fakebun.InstallConfig{
			ExpectedCwd: root,
		},
		Build: &fakebun.BuildConfig{
			Mode:               fakebun.InvocationRun,
			ExpectedCwd:        root,
			ExpectedEntry:      typeScriptBuildBin,
			ExpectedSourceDir:  root,
			ExpectedTarget:     typeScriptTestPluginTarget,
			ExpectedPluginName: "ts-release",
			AllowedPlatforms: []fakebun.Platform{{
				GOOS:   runtime.GOOS,
				GOARCH: runtime.GOARCH,
			}},
			BinaryContent: "#!/bin/sh\n# fake package ts release binary\nexit 0\n",
		},
	})
	t.Setenv(typeScriptBunEnvVar, bunPath)

	outputPath := filepath.Join(t.TempDir(), "gestalt-plugin-ts-release")
	if _, err := BuildSourceProviderReleaseBinary(root, outputPath, "ts-release", runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildSourceProviderReleaseBinary: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", outputPath, err)
	}
	if !containsString(string(data), "fake package ts release binary") {
		t.Fatalf("binary contents = %q, want fake build marker", string(data))
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules", ".installed")); err != nil {
		t.Fatalf("expected bun install marker in provider node_modules: %v", err)
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
			kind:       providermanifestv1.KindAuthentication,
			target:     typeScriptTestAuthTarget,
			pluginName: "ts-auth-release",
		},
		{
			name:       "cache",
			kind:       providermanifestv1.KindCache,
			target:     typeScriptTestCacheTarget,
			pluginName: "ts-cache-release",
		},
		{
			name:       "indexeddb",
			kind:       providermanifestv1.KindIndexedDB,
			target:     typeScriptTestDatastoreTarget,
			pluginName: "ts-datastore-release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mustWriteTypeScriptComponentPackage(t, root, tt.kind, tt.target)
			mustWriteTypeScriptSourceComponentManifest(t, root, tt.pluginName, tt.kind)
			sdkDir := writeFakeTypeScriptSDKDir(t, t.TempDir())
			bunPath := writeFakeTypeScriptBun(t, root, tt.pluginName, tt.target, runtime.GOOS, runtime.GOARCH, sdkDir)
			t.Setenv(typeScriptBunEnvVar, bunPath)
			t.Setenv(typeScriptSDKDirEnvVar, sdkDir)

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
	for _, key := range []string{typeScriptProviderKey, typeScriptPluginKey, typeScriptAuthKey, typeScriptCacheKey, typeScriptIndexedDBKey} {
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
	for _, prefix := range []string{"plugin:", "authentication:", "auth:", "cache:", "indexeddb:", "secrets:", "telemetry:"} {
		if strings.HasPrefix(target, prefix) {
			return strings.TrimPrefix(target, prefix)
		}
	}
	return target
}

func mustWriteTypeScriptSourceManifest(t *testing.T, root, pluginName string) string {
	t.Helper()

	data, err := EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  "github.com/testowner/plugins/" + pluginName,
		Version: "0.0.1",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				},
			},
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

	manifest := &providermanifestv1.Manifest{
		Kind:    kind,
		Source:  "github.com/testowner/plugins/" + pluginName,
		Version: "0.0.1",
		Spec:    &providermanifestv1.Spec{},
	}

	data, err := EncodeSourceManifestFormat(manifest, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}

	path := filepath.Join(root, "manifest.yaml")
	mustWriteFile(t, path, data, 0o644)
	return path
}

func writeFakeTypeScriptBun(t *testing.T, root, pluginName, expectedTarget, expectedGOOS, expectedGOARCH, expectedCWD string) string {
	t.Helper()

	sdkPath := ""
	if expectedCWD != "" {
		if _, err := os.Stat(filepath.Join(expectedCWD, "package.json")); err == nil {
			sdkPath = expectedCWD
		}
	}
	if sdkPath == "" {
		sdkPath = strings.TrimSpace(os.Getenv(typeScriptSDKDirEnvVar))
	}
	if sdkPath == "" {
		sdkPath = fakebun.LocalTypeScriptSDKPath()
	}
	if sdkPath == "" {
		t.Fatal("local TypeScript SDK not found")
	}

	return fakebun.NewExecutable(t, fakebun.Config{
		Install: &fakebun.InstallConfig{
			ExpectedCwds:          []string{root, sdkPath},
			RequireFrozenLockfile: true,
		},
		Runtime: &fakebun.RuntimeConfig{
			ExpectedCwd:    expectedCWD,
			ExpectedEntry:  filepath.Join(sdkPath, "src", "runtime.ts"),
			ExpectedRoot:   root,
			ExpectedTarget: expectedTarget,
			RequireCatalog: true,
			Catalog: fmt.Sprintf(`name: %s
operations:
  - id: greet
    method: GET
`, pluginName),
		},
		Build: &fakebun.BuildConfig{
			ExpectedCwd:        expectedCWD,
			ExpectedEntry:      filepath.Join(sdkPath, "src", "build.ts"),
			ExpectedSourceDir:  root,
			ExpectedTarget:     expectedTarget,
			ExpectedPluginName: pluginName,
			AllowedPlatforms: []fakebun.Platform{{
				GOOS:   expectedGOOS,
				GOARCH: expectedGOARCH,
			}},
			BinaryContent: "#!/bin/sh\n# fake ts release binary\nexit 0\n",
		},
	})
}

func writeFakeTypeScriptSDKDir(t *testing.T, root string) string {
	t.Helper()

	mustWriteFile(t, filepath.Join(root, "package.json"), []byte(`{
  "name": "@valon-technologies/gestalt",
  "version": "0.0.1-alpha.test"
}
`), 0o644)
	mustWriteFile(t, filepath.Join(root, "bun.lock"), []byte("{}\n"), 0o644)
	mustWriteFile(t, filepath.Join(root, "src", "runtime.ts"), []byte("export {};\n"), 0o644)
	mustWriteFile(t, filepath.Join(root, "src", "build.ts"), []byte("export {};\n"), 0o644)
	return root
}
