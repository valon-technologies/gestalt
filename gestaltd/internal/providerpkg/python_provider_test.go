package providerpkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestDetectPythonInterpreter_CurrentPlatformFallsBackToGenericVenv(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pythonPath := pythonTestInterpreterPath(root, runtime.GOOS, ".venv")
	mustWritePythonInterpreter(t, pythonPath)

	got, err := DetectPythonInterpreter(root, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("DetectPythonInterpreter(current): %v", err)
	}
	if got != pythonPath {
		t.Fatalf("interpreter = %q, want %q", got, pythonPath)
	}
}

func TestDetectPythonInterpreter_UsesPlatformSpecificOverride(t *testing.T) {
	root := t.TempDir()
	goos, goarch := pythonTestOtherPlatform()
	pythonPath := filepath.Join(root, "cross-python")
	mustWritePythonInterpreter(t, pythonPath)
	t.Setenv(pythonInterpreterEnvVar(goos, goarch), pythonPath)

	got, err := DetectPythonInterpreter(root, goos, goarch)
	if err != nil {
		t.Fatalf("DetectPythonInterpreter(cross): %v", err)
	}
	if got != pythonPath {
		t.Fatalf("interpreter = %q, want %q", got, pythonPath)
	}
}

func TestDetectPythonInterpreter_CrossTargetRequiresExplicitInterpreter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	goos, goarch := pythonTestOtherPlatform()

	_, err := DetectPythonInterpreter(root, goos, goarch)
	if err == nil {
		t.Fatal("expected cross-target interpreter error")
	}
	if want := pythonInterpreterEnvVar(goos, goarch); !containsString(err.Error(), want) {
		t.Fatalf("error = %q, want mention of %s", err, want)
	}
}

func TestDetectPythonComponentTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
plugin = "provider"
authentication = "provider:auth_provider"
cache = "provider:cache_provider"
indexeddb = "provider:indexeddb_provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}

	authTarget, err := DetectPythonComponentTarget(root, providermanifestv1.KindAuthentication)
	if err != nil {
		t.Fatalf("DetectPythonComponentTarget(auth): %v", err)
	}
	if authTarget != "provider:auth_provider" {
		t.Fatalf("auth target = %q, want %q", authTarget, "provider:auth_provider")
	}

	cacheTarget, err := DetectPythonComponentTarget(root, providermanifestv1.KindCache)
	if err != nil {
		t.Fatalf("DetectPythonComponentTarget(cache): %v", err)
	}
	if cacheTarget != "provider:cache_provider" {
		t.Fatalf("cache target = %q, want %q", cacheTarget, "provider:cache_provider")
	}

	datastoreTarget, err := DetectPythonComponentTarget(root, providermanifestv1.KindIndexedDB)
	if err != nil {
		t.Fatalf("DetectPythonComponentTarget(datastore): %v", err)
	}
	if datastoreTarget != "provider:indexeddb_provider" {
		t.Fatalf("datastore target = %q, want %q", datastoreTarget, "provider:indexeddb_provider")
	}
}

func TestDetectPythonProviderTarget_AcceptsProviderKey(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
provider = "provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}

	target, err := DetectPythonProviderTarget(root)
	if err != nil {
		t.Fatalf("DetectPythonProviderTarget: %v", err)
	}
	if target != "provider" {
		t.Fatalf("target = %q, want %q", target, "provider")
	}
}

func TestDetectPythonProviderTarget_PrefersProviderKeyOverLegacyPluginKey(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
provider = "provider"
plugin = "legacy_provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}

	target, err := DetectPythonProviderTarget(root)
	if err != nil {
		t.Fatalf("DetectPythonProviderTarget: %v", err)
	}
	if target != "provider" {
		t.Fatalf("target = %q, want %q", target, "provider")
	}
}

func TestDetectPythonComponentTarget_MissingKindReturnsNoSourceComponentPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
plugin = "provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}

	_, err := DetectPythonComponentTarget(root, providermanifestv1.KindAuthentication)
	if err == nil {
		t.Fatal("expected missing auth target error")
	}
	if !strings.Contains(err.Error(), ErrNoPythonSourceComponentPackage.Error()) {
		t.Fatalf("error = %q, want %q", err, ErrNoPythonSourceComponentPackage)
	}
}

func TestDetectPythonComponentTarget_RejectsAuthorization(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
authorization = "provider:authorization_provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}

	_, err := DetectPythonComponentTarget(root, providermanifestv1.KindAuthorization)
	if err == nil {
		t.Fatal("expected authorization rejection")
	}
	if !strings.Contains(err.Error(), `unsupported Python runtime kind "authorization"`) {
		t.Fatalf("error = %q, want unsupported Python runtime kind", err)
	}
}

func TestPythonComponentExecutionCommand_PassesRuntimeKind(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pythonPath := pythonTestInterpreterPath(root, runtime.GOOS, ".venv")
	mustWritePythonInterpreter(t, pythonPath)

	command, args, cleanup, err := pythonComponentExecutionCommand(root, "provider:auth_provider", pythonRuntimeKindAuthentication)
	if err != nil {
		t.Fatalf("pythonComponentExecutionCommand: %v", err)
	}
	if cleanup != nil {
		t.Fatal("pythonComponentExecutionCommand cleanup = non-nil, want nil")
	}
	if command != pythonPath {
		t.Fatalf("command = %q, want %q", command, pythonPath)
	}
	if len(args) != 5 {
		t.Fatalf("args = %q, want 5 args", args)
	}
	if got := strings.Join(args, " "); got != "-m gestalt._runtime "+root+" provider:auth_provider authentication" {
		t.Fatalf("args = %q", got)
	}
}

func TestPythonRuntimeKind_Cache(t *testing.T) {
	t.Parallel()

	got, err := pythonRuntimeKind(providermanifestv1.KindCache)
	if err != nil {
		t.Fatalf("pythonRuntimeKind(cache): %v", err)
	}
	if got != pythonRuntimeKindCache {
		t.Fatalf("runtime kind = %q, want %q", got, pythonRuntimeKindCache)
	}
}

func TestSourceProviderExecutionEnv_PythonUsesLocalSDK(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
provider = "provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}

	env, err := SourceProviderExecutionEnv(root, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("SourceProviderExecutionEnv: %v", err)
	}
	if len(env) == 0 {
		t.Fatal("SourceProviderExecutionEnv returned no environment overrides")
	}
	want := localPythonSDKPath()
	if want == "" {
		t.Fatal("localPythonSDKPath returned empty path")
	}
	if got := env["PYTHONPATH"]; !strings.Contains(got, want) {
		t.Fatalf("PYTHONPATH = %q, want to contain %q", got, want)
	}
}

func TestPrepareSourceManifest_MergesGeneratedPythonManifestMetadata(t *testing.T) {
	root := t.TempDir()
	manifestPath := mustWritePythonSourceManifest(t, root, "python-release")
	mustWriteFile(t, filepath.Join(root, pythonProjectFile), []byte(`[tool.gestalt]
provider = "provider:plugin"
`), 0o644)
	mustWriteFile(t, filepath.Join(root, "provider.py"), []byte(`from gestalt import Plugin

plugin = Plugin(
    "python-release",
    securitySchemes={
        "signed": {
            "type": "hmac",
            "secret": {"env": "REQUEST_SIGNING_SECRET"},
            "signatureHeader": "X-Request-Signature",
            "signaturePrefix": "v0=",
            "payloadTemplate": "v0:{header:X-Request-Timestamp}:{raw_body}",
            "timestampHeader": "X-Request-Timestamp",
            "maxAgeSeconds": 300,
        }
    },
    http={
        "command": {
            "path": "/command",
            "method": "POST",
            "security": "signed",
            "target": "handle_command",
            "requestBody": {
                "required": True,
                "content": {
                    "application/x-www-form-urlencoded": {},
                },
            },
            "ack": {
                "status": 200,
                "body": {
                    "status": "accepted",
                },
            },
        }
    },
)

@plugin.operation(id="handle_command")
def handle_command() -> dict[str, str]:
    return {"status": "ok"}
`), 0o644)

	pythonPath, err := pythonTestRuntimePath()
	if err != nil {
		t.Skipf("python runtime unavailable: %v", err)
	}
	t.Setenv("GESTALT_PYTHON", mustWritePythonWithoutGRPCWrapper(t, root, pythonPath))

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
	if scheme.Secret == nil || scheme.Secret.Env != "REQUEST_SIGNING_SECRET" {
		t.Fatalf("scheme.Secret = %+v, want env-backed secret", scheme.Secret)
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
	if binding.Ack == nil {
		t.Fatal("binding.Ack = nil, want ack metadata")
	}
	if binding.Ack.Status != 200 {
		t.Fatalf("binding.Ack.Status = %d, want %d", binding.Ack.Status, 200)
	}
}

func pythonTestOtherPlatform() (string, string) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return "darwin", "arm64"
	}
	return "linux", "amd64"
}

func pythonTestRuntimePath() (string, error) {
	if value := os.Getenv("GESTALT_PYTHON"); value != "" {
		if resolved, err := exec.LookPath(value); err == nil {
			return resolved, nil
		}
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value, nil
		}
	}
	for _, candidate := range []string{"python3", "python"} {
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", exec.ErrNotFound
}

func pythonTestInterpreterPath(root, goos, suffix string) string {
	if goos == "windows" {
		return filepath.Join(root, suffix, "Scripts", "python.exe")
	}
	return filepath.Join(root, suffix, "bin", "python")
}

func mustWritePythonWithoutGRPCWrapper(t *testing.T, root, pythonPath string) string {
	t.Helper()

	path := filepath.Join(root, "python-no-grpc")
	script := fmt.Sprintf(`#!%s
import importlib.abc
import runpy
import sys

class _BlockRuntimeDeps(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        if fullname == "grpc" or fullname.startswith("grpc."):
            error = ModuleNotFoundError("No module named 'grpc'")
            error.name = "grpc"
            raise error
        if fullname == "google" or fullname.startswith("google."):
            error = ModuleNotFoundError("No module named 'google'")
            error.name = "google"
            raise error
        return None

sys.meta_path.insert(0, _BlockRuntimeDeps())

if len(sys.argv) < 3 or sys.argv[1] != "-m":
    raise SystemExit("unsupported invocation")

module_name = sys.argv[2]
sys.argv = [sys.argv[0], *sys.argv[3:]]
runpy.run_module(module_name, run_name="__main__")
`, pythonPath)
	mustWriteFile(t, path, []byte(script), 0o755)
	return path
}

func mustWritePythonInterpreter(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func mustWritePythonSourceManifest(t *testing.T, root, pluginName string) string {
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

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
