// Package transporttest provides transport helpers for SDK fixtures.
package transporttest

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ParseTarget parses the TCP, Unix URL, and plain socket targets used by SDK fixtures.
func ParseTarget(service, raw string) (network string, address string, err error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("%s transport target is required", service)
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("%s tcp target %q is missing host:port", service, raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("%s unix target %q is missing a socket path", service, raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		return "", "", fmt.Errorf("unsupported %s transport target %q", service, raw)
	default:
		return "unix", filepath.Clean(target), nil
	}
}
