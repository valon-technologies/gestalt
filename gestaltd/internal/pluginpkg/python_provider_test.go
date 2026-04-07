package pluginpkg

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
