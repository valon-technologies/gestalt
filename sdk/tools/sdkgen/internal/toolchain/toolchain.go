// Package toolchain resolves and runs the external binaries sdkgen depends
// on, verifying version pins before any file is written.
package toolchain

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Tool is an external binary sdkgen depends on. When Version is non-empty the
// binary must report exactly that version.
type Tool struct {
	Name        string
	Version     string
	VersionArgs []string
	// FormatArgs are prepended to file paths when the tool runs as a
	// formatter over emitted files.
	FormatArgs  []string
	InstallHint string

	resolved string
}

// Path resolves the tool binary from PATH, falling back to GOPATH/bin where
// CI installs buf without extending PATH.
func (t *Tool) Path() (string, error) {
	if t.resolved != "" {
		return t.resolved, nil
	}
	if p, err := exec.LookPath(t.Name); err == nil {
		t.resolved = p
		return p, nil
	}
	if gopath := gopath(); gopath != "" {
		p := filepath.Join(gopath, "bin", t.Name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			t.resolved = p
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found on PATH (%s)", t.Name, t.InstallHint)
}

// Verify checks that the tool exists and, when pinned, reports the exact
// pinned version.
func (t *Tool) Verify() error {
	path, err := t.Path()
	if err != nil {
		return err
	}
	if t.Version == "" {
		return nil
	}
	out, err := exec.Command(path, t.VersionArgs...).Output()
	if err != nil {
		return fmt.Errorf("%s %s: %w", t.Name, strings.Join(t.VersionArgs, " "), err)
	}
	got := strings.TrimSpace(string(out))
	if got != t.Version {
		return fmt.Errorf("%s version mismatch: need %s, found %s (%s)", t.Name, t.Version, got, t.InstallHint)
	}
	return nil
}

// Run executes the tool in dir, returning stderr in the error on failure.
func (t *Tool) Run(dir string, args ...string) error {
	path, err := t.Path()
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w\n%s", t.Name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

var gopath = sync.OnceValue(func() string {
	out, err := exec.Command("go", "env", "GOPATH").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
})
