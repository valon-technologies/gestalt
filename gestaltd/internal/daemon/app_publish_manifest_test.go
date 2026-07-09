package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initAppPublishGitRepo(t *testing.T, dir, remote string) {
	t.Helper()

	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", remote},
	} {
		out, err := runProviderPublishCommand("git", append([]string{"-C", dir}, args...)...)
		if err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func TestResolveAppPublishManifest(t *testing.T) {
	rootDir := t.TempDir()
	initAppPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")
	appDir := filepath.Join(rootDir, "valon-tools", "apps", "g-issues")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifestPath := filepath.Join(appDir, appPublishManifestFile)
	if err := os.WriteFile(manifestPath, []byte("kind: app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Chdir(rootDir)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	got, err := resolveAppPublishManifest("g-issues")
	if err != nil {
		t.Fatalf("resolveAppPublishManifest: %v", err)
	}
	want, err := filepath.EvalSymlinks(manifestPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(want): %v", err)
	}
	got, err = filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(got): %v", err)
	}
	if got != want {
		t.Fatalf("resolveAppPublishManifest = %q, want %q", got, want)
	}
}

func TestResolveAppPublishManifestRequiresUniqueMatch(t *testing.T) {
	rootDir := t.TempDir()
	initAppPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")
	for _, rel := range []string{
		"apps/g-issues/manifest.yaml",
		"valon-tools/apps/g-issues/manifest.yaml",
	} {
		path := filepath.Join(rootDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte("kind: app\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Chdir(rootDir)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	_, err = resolveAppPublishManifest("g-issues")
	if err == nil {
		t.Fatal("expected ambiguous manifest error")
	}
	if !strings.Contains(err.Error(), "multiple apps/g-issues/manifest.yaml") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveAppPublishManifestNotFoundIsActionable(t *testing.T) {
	rootDir := t.TempDir()
	initAppPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Chdir(rootDir)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	_, err = resolveAppPublishManifest("missing-app")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !strings.Contains(err.Error(), "no apps/missing-app/manifest.yaml") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "verify --app") {
		t.Fatalf("error = %v, want actionable hint", err)
	}
}

func TestResolveAppPublishManifestHintsWhenOnlyManifestJSONExists(t *testing.T) {
	rootDir := t.TempDir()
	initAppPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")
	appDir := filepath.Join(rootDir, "apps", "g-issues")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Chdir(rootDir)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	_, err = resolveAppPublishManifest("g-issues")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !strings.Contains(err.Error(), "requires manifest.yaml") {
		t.Fatalf("error = %v", err)
	}
}
