package packageio

import (
	"fmt"
	"runtime"
	"strings"
)

func PlatformString(goos, goarch string) string {
	return fmt.Sprintf("%s/%s", goos, goarch)
}

func PlatformArchiveSuffix(goos, goarch string) string {
	return fmt.Sprintf("%s_%s", goos, goarch)
}

// CurrentPlatformString returns the platform string for the host running
// this process (e.g. "darwin/arm64", "linux/amd64").
func CurrentPlatformString() string {
	return PlatformString(runtime.GOOS, runtime.GOARCH)
}

// ParsePlatformString parses "darwin/arm64" or "linux/amd64" into (goos, goarch).
func ParsePlatformString(s string) (goos, goarch string, err error) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid platform string %q: expected os/arch", s)
	}
	if parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid platform string %q: empty component", s)
	}
	return parts[0], parts[1], nil
}
