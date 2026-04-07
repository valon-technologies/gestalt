package pluginpkg

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const (
	LinuxLibCGLibC = "glibc"
	LinuxLibCMusl  = "musl"
)

func NormalizeArtifactLibC(goos, libc string) (string, error) {
	libc = strings.TrimSpace(strings.ToLower(libc))
	if goos != "linux" {
		if libc != "" {
			return "", fmt.Errorf("artifact libc %q is only supported for linux artifacts", libc)
		}
		return "", nil
	}
	switch libc {
	case "", LinuxLibCGLibC, LinuxLibCMusl:
		return libc, nil
	default:
		return "", fmt.Errorf("unsupported linux libc %q (want %q or %q)", libc, LinuxLibCGLibC, LinuxLibCMusl)
	}
}

func CurrentRuntimeLibC() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	out, err := exec.Command("ldd", "--version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	lower := strings.ToLower(string(out))
	switch {
	case strings.Contains(lower, LinuxLibCMusl):
		return LinuxLibCMusl
	case strings.Contains(lower, LinuxLibCGLibC), strings.Contains(lower, "gnu libc"), strings.Contains(lower, "gnu c library"):
		return LinuxLibCGLibC
	default:
		return ""
	}
}

func PlatformString(goos, goarch, libc string) string {
	if goos == "linux" && libc != "" {
		return fmt.Sprintf("%s/%s/%s", goos, goarch, libc)
	}
	return fmt.Sprintf("%s/%s", goos, goarch)
}

func PlatformArchiveSuffix(goos, goarch, libc string) string {
	if goos == "linux" && libc != "" {
		return fmt.Sprintf("%s_%s_%s", goos, goarch, libc)
	}
	return fmt.Sprintf("%s_%s", goos, goarch)
}
