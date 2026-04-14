package providerpkg

import (
	"os"
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
auth = "provider:auth_provider"
fileapi = "provider:fileapi_provider"
indexeddb = "provider:indexeddb_provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}

	authTarget, err := DetectPythonComponentTarget(root, providermanifestv1.KindAuth)
	if err != nil {
		t.Fatalf("DetectPythonComponentTarget(auth): %v", err)
	}
	if authTarget != "provider:auth_provider" {
		t.Fatalf("auth target = %q, want %q", authTarget, "provider:auth_provider")
	}

	fileAPITarget, err := DetectPythonComponentTarget(root, providermanifestv1.KindFileAPI)
	if err != nil {
		t.Fatalf("DetectPythonComponentTarget(fileapi): %v", err)
	}
	if fileAPITarget != "provider:fileapi_provider" {
		t.Fatalf("fileapi target = %q, want %q", fileAPITarget, "provider:fileapi_provider")
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

	_, err := DetectPythonComponentTarget(root, providermanifestv1.KindAuth)
	if err == nil {
		t.Fatal("expected missing auth target error")
	}
	if !strings.Contains(err.Error(), ErrNoPythonSourceComponentPackage.Error()) {
		t.Fatalf("error = %q, want %q", err, ErrNoPythonSourceComponentPackage)
	}
}

func TestPythonComponentExecutionCommand_PassesRuntimeKind(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pythonPath := pythonTestInterpreterPath(root, runtime.GOOS, ".venv")
	mustWritePythonInterpreter(t, pythonPath)

	command, args, cleanup, err := pythonComponentExecutionCommand(root, "provider:fileapi_provider", pythonRuntimeKindFileAPI)
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
	if got := strings.Join(args, " "); got != "-m gestalt._runtime "+root+" provider:fileapi_provider fileapi" {
		t.Fatalf("args = %q", got)
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

func pythonTestOtherPlatform() (string, string) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return "darwin", "arm64"
	}
	return "linux", "amd64"
}

func pythonTestInterpreterPath(root, goos, suffix string) string {
	if goos == "windows" {
		return filepath.Join(root, suffix, "Scripts", "python.exe")
	}
	return filepath.Join(root, suffix, "bin", "python")
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

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
