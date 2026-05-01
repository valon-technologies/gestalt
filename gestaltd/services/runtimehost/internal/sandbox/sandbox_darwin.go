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
	b.WriteString("(allow file-read-metadata)\n")
	b.WriteString("(allow file-read-data (literal \"/\"))\n")

	for _, p := range policy.ReadOnlyPaths {
		sbplAllow(&b, "file-read*", p)
	}

	for _, p := range policy.ReadWritePaths {
		sbplAllow(&b, "file-read*", p)
		sbplAllow(&b, "file-write*", p)
	}

	if policy.ProxyPort > 0 {
		fmt.Fprintf(&b, "(allow network-outbound (remote tcp %s))\n", sbplQuote(fmt.Sprintf("localhost:%d", policy.ProxyPort)))
		b.WriteString("(allow network* (local unix-socket))\n")
	} else {
		b.WriteString("(allow network*)\n")
	}

	return b.String()
}

func sbplAllow(b *strings.Builder, access, p string) {
	sbplAllowOnePath(b, access, p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil && resolved != p {
		sbplAllowOnePath(b, access, resolved)
	}
}

func sbplAllowOnePath(b *strings.Builder, access, p string) {
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		fmt.Fprintf(b, "(allow %s (literal %s))\n", access, sbplQuote(p))
		return
	}
	fmt.Fprintf(b, "(allow %s (subpath %s))\n", access, sbplQuote(p))
}

func sbplQuote(path string) string {
	escaped := strings.ReplaceAll(path, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	return "\"" + escaped + "\""
}
