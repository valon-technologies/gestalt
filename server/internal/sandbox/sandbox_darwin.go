//go:build darwin

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Wrap(policy *Policy, cmd *exec.Cmd) (*exec.Cmd, func(), error) {
	profile := buildSBPLProfile(policy)
	tmp, err := os.CreateTemp("", "gestalt-sandbox-*.sb")
	if err != nil {
		return nil, nil, fmt.Errorf("create sandbox profile: %w", err)
	}
	if _, err := tmp.WriteString(profile); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, nil, fmt.Errorf("write sandbox profile: %w", err)
	}
	_ = tmp.Close()

	profilePath := tmp.Name()
	cleanup := func() { _ = os.Remove(profilePath) }

	args := []string{"-f", profilePath, cmd.Path}
	args = append(args, cmd.Args[1:]...)
	wrapped := exec.Command("sandbox-exec", args...)
	wrapped.Env = cmd.Env
	wrapped.Dir = cmd.Dir
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr
	wrapped.SysProcAttr = cmd.SysProcAttr
	return wrapped, cleanup, nil
}

func RunSubcommand(_ []string) error {
	return fmt.Errorf("sandbox subcommand is not supported on darwin")
}

func DefaultReadOnlyPaths() []string {
	candidates := []string{
		"/usr/lib",
		"/usr/share",
		"/System",
		"/Library",
		"/private/var/db",
		"/dev/urandom",
		"/dev/null",
	}
	var paths []string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

func buildSBPLProfile(policy *Policy) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(allow process*)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach*)\n")
	b.WriteString("(allow file-read*)\n")

	for _, p := range policy.ReadWritePaths {
		sbplAllowWrite(&b, p)
	}

	if policy.ProxyPort > 0 {
		b.WriteString("(allow network-outbound (remote ip \"localhost:*\"))\n")
		b.WriteString("(allow network* (local unix-socket))\n")
	} else {
		b.WriteString("(allow network*)\n")
	}

	return b.String()
}

func sbplAllowWrite(b *strings.Builder, p string) {
	b.WriteString(fmt.Sprintf("(allow file-write* (subpath %s))\n", sbplQuote(p)))
	if resolved, err := filepath.EvalSymlinks(p); err == nil && resolved != p {
		b.WriteString(fmt.Sprintf("(allow file-write* (subpath %s))\n", sbplQuote(resolved)))
	}
}

func sbplQuote(path string) string {
	escaped := strings.ReplaceAll(path, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	return "\"" + escaped + "\""
}
