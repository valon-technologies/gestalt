//go:build darwin

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSBPLProfileRestrictsFileReadsToDeclaredPaths(t *testing.T) {
	root := t.TempDir()
	roDir := filepath.Join(root, "ro-dir")
	rwDir := filepath.Join(root, "rw-dir")
	roFile := filepath.Join(root, "ro-file")
	rwFile := filepath.Join(root, "rw-file")

	if err := os.Mkdir(roDir, 0o755); err != nil {
		t.Fatalf("mkdir ro dir: %v", err)
	}
	if err := os.Mkdir(rwDir, 0o755); err != nil {
		t.Fatalf("mkdir rw dir: %v", err)
	}
	if err := os.WriteFile(roFile, []byte("ro"), 0o644); err != nil {
		t.Fatalf("write ro file: %v", err)
	}
	if err := os.WriteFile(rwFile, []byte("rw"), 0o644); err != nil {
		t.Fatalf("write rw file: %v", err)
	}

	profile := buildSBPLProfile(&Policy{
		ReadOnlyPaths:  []string{roDir, roFile},
		ReadWritePaths: []string{rwDir, rwFile},
	})

	if strings.Contains(profile, "(allow file-read*)\n") {
		t.Fatalf("profile contains blanket file-read allowance:\n%s", profile)
	}

	mustContain(t, profile, "(allow file-read* (subpath "+sbplQuote(roDir)+"))")
	mustContain(t, profile, "(allow file-read* (literal "+sbplQuote(roFile)+"))")
	mustContain(t, profile, "(allow file-read* (subpath "+sbplQuote(rwDir)+"))")
	mustContain(t, profile, "(allow file-read* (literal "+sbplQuote(rwFile)+"))")
	mustContain(t, profile, "(allow file-write* (subpath "+sbplQuote(rwDir)+"))")
	mustContain(t, profile, "(allow file-write* (literal "+sbplQuote(rwFile)+"))")

	if strings.Contains(profile, "(allow file-write* (subpath "+sbplQuote(roDir)+"))") {
		t.Fatalf("read-only directory should not receive write access:\n%s", profile)
	}
	if strings.Contains(profile, "(allow file-write* (literal "+sbplQuote(roFile)+"))") {
		t.Fatalf("read-only file should not receive write access:\n%s", profile)
	}
}

func TestBuildSBPLProfileAllowsSymlinkTargets(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "target-dir")
	targetFile := filepath.Join(root, "target-file")
	linkDir := filepath.Join(root, "link-dir")
	linkFile := filepath.Join(root, "link-file")

	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(targetFile, []byte("target"), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	if err := os.Symlink(targetDir, linkDir); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}
	if err := os.Symlink(targetFile, linkFile); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	resolvedDir, err := filepath.EvalSymlinks(linkDir)
	if err != nil {
		t.Fatalf("resolve dir symlink: %v", err)
	}
	resolvedFile, err := filepath.EvalSymlinks(linkFile)
	if err != nil {
		t.Fatalf("resolve file symlink: %v", err)
	}

	profile := buildSBPLProfile(&Policy{
		ReadOnlyPaths:  []string{linkDir},
		ReadWritePaths: []string{linkFile},
	})

	mustContain(t, profile, "(allow file-read* (subpath "+sbplQuote(linkDir)+"))")
	mustContain(t, profile, "(allow file-read* (subpath "+sbplQuote(resolvedDir)+"))")
	mustContain(t, profile, "(allow file-read* (literal "+sbplQuote(linkFile)+"))")
	mustContain(t, profile, "(allow file-read* (literal "+sbplQuote(resolvedFile)+"))")
	mustContain(t, profile, "(allow file-write* (literal "+sbplQuote(linkFile)+"))")
	mustContain(t, profile, "(allow file-write* (literal "+sbplQuote(resolvedFile)+"))")
}

func mustContain(t *testing.T, profile, want string) {
	t.Helper()
	if !strings.Contains(profile, want) {
		t.Fatalf("profile missing %q:\n%s", want, profile)
	}
}
