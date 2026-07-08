package toolchain

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeTool writes an executable script that prints version and returns a Tool
// pre-resolved to it, avoiding PATH manipulation so tests can run in parallel.
func fakeTool(t *testing.T, version string) *Tool {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake tool scripts are POSIX-only")
	}
	path := filepath.Join(t.TempDir(), "fakebuf")
	script := "#!/bin/sh\necho " + version + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Tool{
		Name:        "fakebuf",
		VersionArgs: []string{"--version"},
		resolved:    path,
	}
}

func TestVerifyAcceptsPinnedVersion(t *testing.T) {
	tool := fakeTool(t, "9.9.9")
	tool.Version = "9.9.9"
	if err := tool.Verify(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyRejectsVersionMismatch(t *testing.T) {
	tool := fakeTool(t, "9.9.9")
	tool.Version = "1.2.3"
	tool.InstallHint = "install hint"
	err := tool.Verify()
	if err == nil {
		t.Fatal("verify accepted mismatched version")
	}
	for _, want := range []string{"version mismatch", "need 1.2.3", "found 9.9.9", "install hint"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestVerifyReportsMissingTool(t *testing.T) {
	t.Parallel()
	tool := &Tool{Name: "sdkgen-test-tool-that-does-not-exist", Version: "1.0.0", InstallHint: "install hint"}
	err := tool.Verify()
	if err == nil {
		t.Fatal("verify accepted missing tool")
	}
	if !strings.Contains(err.Error(), "install hint") {
		t.Errorf("error %q missing install hint", err)
	}
}
