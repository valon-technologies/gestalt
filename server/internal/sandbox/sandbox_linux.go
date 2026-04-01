//go:build linux

package sandbox

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
)

func Wrap(policy *Policy, cmd *exec.Cmd) (*exec.Cmd, func(), error) {
	hostBin := policy.HostBinary
	if hostBin == "" {
		var err error
		hostBin, err = os.Executable()
		if err != nil {
			return nil, nil, fmt.Errorf("resolve host binary: %w", err)
		}
	}

	args := []string{hostBin, "__sandbox"}
	for _, p := range policy.ReadOnlyPaths {
		args = append(args, "--ro="+p)
	}
	for _, p := range policy.ReadWritePaths {
		args = append(args, "--rw="+p)
	}
	if policy.ProxyPort > 0 {
		args = append(args, "--proxy-port="+strconv.Itoa(policy.ProxyPort))
	}
	remaining := append([]string{"--", cmd.Path}, cmd.Args[1:]...)
	args = append(args, remaining...)

	wrapped := exec.Command(args[0], args[1:]...)
	wrapped.Env = cmd.Env
	wrapped.Dir = cmd.Dir
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr
	wrapped.SysProcAttr = cmd.SysProcAttr
	return wrapped, func() {}, nil
}

func RunSubcommand(args []string) error {
	fs := flag.NewFlagSet("__sandbox", flag.ContinueOnError)

	var roPaths, rwPaths multiFlag
	var proxyPort int

	fs.Var(&roPaths, "ro", "read-only path")
	fs.Var(&rwPaths, "rw", "read-write path")
	fs.IntVar(&proxyPort, "proxy-port", 0, "proxy port for network restriction")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse sandbox flags: %w", err)
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("no command specified after sandbox flags")
	}

	binary, err := exec.LookPath(remaining[0])
	if err != nil {
		return fmt.Errorf("look up binary %q: %w", remaining[0], err)
	}

	var allRules []landlock.Rule
	for _, p := range roPaths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			allRules = append(allRules, landlock.RODirs(p))
		} else {
			allRules = append(allRules, landlock.ROFiles(p))
		}
	}
	for _, p := range rwPaths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			allRules = append(allRules, landlock.RWDirs(p))
		} else {
			allRules = append(allRules, landlock.RWFiles(p))
		}
	}

	if err := landlock.V5.RestrictPaths(allRules...); err != nil {
		return fmt.Errorf("landlock restrict paths: %w", err)
	}

	if proxyPort > 0 {
		if err := landlock.V4.RestrictNet(landlock.ConnectTCP(uint16(proxyPort))); err != nil {
			slog.Warn("sandbox: landlock network restriction unavailable", "error", err)
		}
	}

	return syscall.Exec(binary, remaining, os.Environ())
}

func DefaultReadOnlyPaths() []string {
	candidates := []string{
		"/usr/lib",
		"/usr/lib64",
		"/usr/share",
		"/lib",
		"/lib64",
		"/etc/ssl",
		"/etc/ca-certificates",
		"/etc/resolv.conf",
		"/etc/nsswitch.conf",
		"/etc/hosts",
		"/dev/urandom",
		"/dev/null",
		"/proc/self",
	}
	var paths []string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

type multiFlag []string

func (f *multiFlag) String() string { return strings.Join(*f, ",") }
func (f *multiFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}
