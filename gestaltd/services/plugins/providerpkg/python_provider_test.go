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
provider = "provider"
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

func TestDetectPythonComponentTarget_MissingKindReturnsNoSourceComponentPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
provider = "provider"
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

func TestSourceProviderExecutionEnv_PythonClearsPythonPathByDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
provider = "provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}
	t.Setenv(pythonSDKDirEnvVar, "")
	t.Setenv("PYTHONPATH", filepath.Join(t.TempDir(), "ambient-sdk"))

	env, err := SourceProviderExecutionEnv(root, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("SourceProviderExecutionEnv: %v", err)
	}
	if got, ok := env["PYTHONPATH"]; !ok {
		t.Fatal("SourceProviderExecutionEnv did not clear PYTHONPATH")
	} else if got != "" {
		t.Fatalf("PYTHONPATH = %q, want empty", got)
	}
}

func TestSourceProviderExecutionEnv_PythonUsesExplicitSDKDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
provider = "provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}
	sdkDir := writePythonSDKDir(t)
	inheritedPath := filepath.Join(t.TempDir(), "ambient-pythonpath")
	t.Setenv(pythonSDKDirEnvVar, sdkDir)
	t.Setenv("PYTHONPATH", inheritedPath)

	env, err := SourceProviderExecutionEnv(root, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("SourceProviderExecutionEnv: %v", err)
	}
	parts := filepath.SplitList(env["PYTHONPATH"])
	if len(parts) != 2 {
		t.Fatalf("PYTHONPATH = %q, want explicit SDK plus inherited path", env["PYTHONPATH"])
	}
	if parts[0] != sdkDir {
		t.Fatalf("PYTHONPATH first entry = %q, want %q", parts[0], sdkDir)
	}
	if parts[1] != inheritedPath {
		t.Fatalf("PYTHONPATH inherited entry = %q, want %q", parts[1], inheritedPath)
	}
}

func TestSourceProviderExecutionEnv_PythonRejectsInvalidExplicitSDKDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
provider = "provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}
	t.Setenv(pythonSDKDirEnvVar, filepath.Join(t.TempDir(), "missing-sdk"))

	_, err := SourceProviderExecutionEnv(root, runtime.GOOS, runtime.GOARCH)
	if err == nil {
		t.Fatal("expected invalid explicit SDK dir error")
	}
	if !strings.Contains(err.Error(), pythonSDKDirEnvVar) {
		t.Fatalf("error = %q, want %s", err, pythonSDKDirEnvVar)
	}
}

func writePythonSDKDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(`[project]
name = "gestalt-sdk"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "gestalt"), 0o755); err != nil {
		t.Fatalf("Mkdir(gestalt): %v", err)
	}
	return dir
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
