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

const allowNetworkRestrictionUnavailableEnv = "GESTALTD_SANDBOX_ALLOW_NETWORK_RESTRICTION_UNAVAILABLE"

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
	wrapped.Env = sandboxWrapperEnv(cmd.Env)
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
			if !allowNetworkRestrictionUnavailable() {
				return fmt.Errorf("landlock restrict network to proxy port %d: %w", proxyPort, err)
			}
			slog.Warn("sandbox: landlock network restriction unavailable",
				"error", err,
				"override_env", allowNetworkRestrictionUnavailableEnv,
			)
		}
	}

	return syscall.Exec(binary, remaining, sandboxExecEnv())
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

func allowNetworkRestrictionUnavailable() bool {
	value := strings.TrimSpace(os.Getenv(allowNetworkRestrictionUnavailableEnv))
	return value == "1" || strings.EqualFold(value, "true")
}

func sandboxWrapperEnv(pluginEnv []string) []string {
	baseEnv := pluginEnv
	if baseEnv == nil {
		baseEnv = os.Environ()
	}
	env := filterEnvKey(baseEnv, allowNetworkRestrictionUnavailableEnv)
	if allowNetworkRestrictionUnavailable() {
		env = append(env, allowNetworkRestrictionUnavailableEnv+"="+os.Getenv(allowNetworkRestrictionUnavailableEnv))
	}
	return env
}

func sandboxExecEnv() []string {
	return filterEnvKey(os.Environ(), allowNetworkRestrictionUnavailableEnv)
}

func filterEnvKey(env []string, key string) []string {
	prefix := key + "="
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
