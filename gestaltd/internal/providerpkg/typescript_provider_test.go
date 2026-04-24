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
	if len(args) != 6 {
		t.Fatalf("args len = %d, want 6 (%v)", len(args), args)
	}
	if args[0] != "--cwd" || args[1] != root {
		t.Fatalf("args prefix = %v, want [--cwd %s ...]", args[:2], root)
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

func TestPrepareSourceManifest_MergesGeneratedTypeScriptManifestMetadata(t *testing.T) {
	root := t.TempDir()
	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  "github.com/testowner/plugins/ts-release",
		Version: "0.0.1",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				},
			},
			SecuritySchemes: map[string]*providermanifestv1.HTTPSecurityScheme{
				"signed": {
					Type: providermanifestv1.HTTPSecuritySchemeTypeHMAC,
					Secret: &providermanifestv1.HTTPSecretRef{
						Env: "OLD_SIGNING_SECRET",
					},
					SignatureHeader: "X-Old-Signature",
					SignaturePrefix: "v1=",
					PayloadTemplate: "v1:{header:X-Old-Timestamp}:{raw_body}",
					TimestampHeader: "X-Old-Timestamp",
					MaxAgeSeconds:   30,
				},
			},
		},
	}
	data, err := EncodeSourceManifestFormat(manifest, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	manifestPath := filepath.Join(root, "manifest.yaml")
	mustWriteFile(t, manifestPath, data, 0o644)
	mustWriteTypeScriptProviderPackage(t, root, typeScriptTestPluginTarget)
	bunPath := writeFakeTypeScriptBun(t, root, "ts-release", typeScriptTestPluginTarget, runtime.GOOS, runtime.GOARCH)
	t.Setenv(typeScriptBunEnvVar, bunPath)

	preparedData, preparedManifest, err := PrepareSourceManifest(manifestPath)
	if err != nil {
		t.Fatalf("PrepareSourceManifest: %v", err)
	}
	if preparedManifest == nil || preparedManifest.Spec == nil {
		t.Fatalf("prepared manifest = %+v, want provider metadata", preparedManifest)
	}
	if !containsString(string(preparedData), "securitySchemes:") {
		t.Fatalf("prepared manifest data = %q, want merged security scheme metadata", string(preparedData))
	}
	if !containsString(string(preparedData), "path: /command") {
		t.Fatalf("prepared manifest data = %q, want merged HTTP binding metadata", string(preparedData))
	}

	scheme := preparedManifest.Spec.SecuritySchemes["signed"]
	if scheme == nil {
		t.Fatal(`manifest.Spec.SecuritySchemes["signed"] = nil, want generated scheme`)
	}
	if scheme.Type != providermanifestv1.HTTPSecuritySchemeTypeHMAC {
		t.Fatalf("scheme.Type = %q, want %q", scheme.Type, providermanifestv1.HTTPSecuritySchemeTypeHMAC)
	}
	if scheme.Secret == nil || scheme.Secret.Env != "OLD_SIGNING_SECRET" {
		t.Fatalf("scheme.Secret = %+v, want manifest override secret", scheme.Secret)
	}
	if scheme.SignatureHeader != "X-Old-Signature" {
		t.Fatalf("scheme.SignatureHeader = %q, want %q", scheme.SignatureHeader, "X-Old-Signature")
	}
	if scheme.SignaturePrefix != "v1=" {
		t.Fatalf("scheme.SignaturePrefix = %q, want %q", scheme.SignaturePrefix, "v1=")
	}
	if scheme.PayloadTemplate != "v1:{header:X-Old-Timestamp}:{raw_body}" {
		t.Fatalf("scheme.PayloadTemplate = %q, want %q", scheme.PayloadTemplate, "v1:{header:X-Old-Timestamp}:{raw_body}")
	}
	if scheme.TimestampHeader != "X-Old-Timestamp" {
		t.Fatalf("scheme.TimestampHeader = %q, want %q", scheme.TimestampHeader, "X-Old-Timestamp")
	}
	if scheme.MaxAgeSeconds != 30 {
		t.Fatalf("scheme.MaxAgeSeconds = %d, want %d", scheme.MaxAgeSeconds, 30)
	}

	binding := preparedManifest.Spec.HTTP["command"]
	if binding == nil {
		t.Fatal(`manifest.Spec.HTTP["command"] = nil, want generated binding`)
	}
	if binding.Path != "/command" {
		t.Fatalf("binding.Path = %q, want %q", binding.Path, "/command")
	}
	if binding.Method != "POST" {
		t.Fatalf("binding.Method = %q, want %q", binding.Method, "POST")
	}
	if binding.Security != "signed" {
		t.Fatalf("binding.Security = %q, want %q", binding.Security, "signed")
	}
	if binding.Target != "handle_command" {
		t.Fatalf("binding.Target = %q, want %q", binding.Target, "handle_command")
	}
	if binding.RequestBody == nil {
		t.Fatal("binding.RequestBody = nil, want request body metadata")
	}
	if _, ok := binding.RequestBody.Content["application/x-www-form-urlencoded"]; !ok {
		t.Fatalf("binding.RequestBody.Content = %#v, want form content type", binding.RequestBody.Content)
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

func writeFakeTypeScriptBun(t *testing.T, root, pluginName, expectedTarget, expectedGOOS, expectedGOARCH string) string {
	t.Helper()

	sdkPath := fakebun.LocalTypeScriptSDKPath()
	if sdkPath == "" {
		t.Fatal("local TypeScript SDK not found")
	}

	return fakebun.NewExecutable(t, fakebun.Config{
		Runtime: &fakebun.RuntimeConfig{
			ExpectedCwd:    root,
			ExpectedEntry:  filepath.Join(sdkPath, "src", "runtime.ts"),
			ExpectedRoot:   root,
			ExpectedTarget: expectedTarget,
			RequireCatalog: true,
			Catalog: fmt.Sprintf(`name: %s
operations:
  - id: greet
    method: GET
`, pluginName),
			ManifestMetadata: `securitySchemes:
  signed:
    type: hmac
    secret:
      env: REQUEST_SIGNING_SECRET
    signatureHeader: X-Request-Signature
    signaturePrefix: v0=
    payloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}"
    timestampHeader: X-Request-Timestamp
    maxAgeSeconds: 300
http:
  command:
    path: /command
    method: POST
    security: signed
    target: handle_command
    requestBody:
      required: true
      content:
        application/x-www-form-urlencoded: {}
`,
		},
		Build: &fakebun.BuildConfig{
			ExpectedCwd:        root,
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
