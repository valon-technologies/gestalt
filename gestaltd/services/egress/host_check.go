package egress

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// PolicyAction is the server-wide default for outbound requests.
type PolicyAction string

const (
	PolicyAllow PolicyAction = "allow"
	PolicyDeny  PolicyAction = "deny"
)

type Policy struct {
	AllowedHosts  []string
	DefaultAction PolicyAction
}

func (p Policy) CheckHost(host string) error {
	return CheckHost(p.AllowedHosts, host, p.DefaultAction)
}

func (p Policy) CheckEndpoint(hostport, defaultPort string) error {
	return CheckEndpoint(p.AllowedHosts, hostport, p.DefaultAction, defaultPort)
}

func (p Policy) RequiresHostnameEnforcement() bool {
	return len(p.AllowedHosts) > 0 || p.DefaultAction == PolicyDeny
}

// ErrEgressDenied is returned when an outbound request is blocked by policy.
var ErrEgressDenied = errors.New("egress denied")

// CheckHost reports whether an outbound request to host is permitted given
// the provider's allowed-host list and the server-wide default action.
//
// When allowedHosts is non-empty only listed hosts are reachable (exact match
// and *.suffix wildcards). When empty, the server-wide defaultAction decides.
func CheckHost(allowedHosts []string, host string, defaultAction PolicyAction) error {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if len(allowedHosts) > 0 {
		if matchHost(allowedHosts, host) {
			return nil
		}
		return fmt.Errorf("%w: host %q is not in the allowed list", ErrEgressDenied, host)
	}
	if defaultAction == PolicyDeny {
		return fmt.Errorf("%w: host %q denied by default policy", ErrEgressDenied, host)
	}
	return nil
}

// CheckEndpoint reports whether an outbound request to hostport is permitted
// by the host allowlist and port policy. Allowlist entries may include an
// explicit port. Entries without a port permit only the request's default port,
// except explicit localhost/loopback entries which keep local development
// compatibility and allow any loopback port.
func CheckEndpoint(allowedHosts []string, hostport string, defaultAction PolicyAction, defaultPort string) error {
	host, port, err := SplitHostPortDefault(hostport, defaultPort)
	if err != nil {
		return err
	}
	if len(allowedHosts) > 0 {
		if matchEndpoint(allowedHosts, host, port, defaultPort) {
			return nil
		}
		return fmt.Errorf("%w: endpoint %q is not in the allowed list", ErrEgressDenied, net.JoinHostPort(host, port))
	}
	if defaultAction == PolicyDeny {
		return fmt.Errorf("%w: host %q denied by default policy", ErrEgressDenied, host)
	}
	return nil
}

// matchHost checks host against patterns supporting exact and *.suffix forms.
func matchHost(patterns []string, host string) bool {
	lower := strings.ToLower(host)
	for _, pattern := range patterns {
		p := strings.ToLower(pattern)
		if p == lower {
			return true
		}
		if strings.HasPrefix(p, "*.") && strings.HasSuffix(lower, p[1:]) {
			return true
		}
	}
	return false
}

func matchEndpoint(patterns []string, host, port, defaultPort string) bool {
	for _, pattern := range patterns {
		patternHost, patternPort, hasPort, err := splitAllowedHostPattern(pattern)
		if err != nil {
			continue
		}
		if !matchHost([]string{patternHost}, host) {
			continue
		}
		if hasPort {
			if port == patternPort {
				return true
			}
			continue
		}
		if defaultPort != "" && port == defaultPort {
			return true
		}
		if IsLocalhostName(patternHost) && IsLocalhostName(host) {
			return true
		}
	}
	return false
}

func splitAllowedHostPattern(pattern string) (host string, port string, hasPort bool, err error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", "", false, fmt.Errorf("allowed host pattern is empty")
	}
	if h, p, splitErr := net.SplitHostPort(pattern); splitErr == nil {
		if err := validatePort(p); err != nil {
			return "", "", false, err
		}
		return normalizeHost(h), p, true, nil
	}
	return normalizeHost(pattern), "", false, nil
}

// SplitHostPortDefault splits hostport, applying defaultPort when the port is
// absent. The returned host is normalized for matching and dialing.
func SplitHostPortDefault(hostport, defaultPort string) (host string, port string, err error) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "", "", fmt.Errorf("%w: target host is required", ErrEgressDenied)
	}
	if h, p, splitErr := net.SplitHostPort(hostport); splitErr == nil {
		if err := validatePort(p); err != nil {
			return "", "", err
		}
		return normalizeHost(h), p, nil
	}
	if defaultPort == "" {
		return "", "", fmt.Errorf("%w: endpoint %q is missing a port", ErrEgressDenied, hostport)
	}
	if err := validatePort(defaultPort); err != nil {
		return "", "", err
	}
	return normalizeHost(hostport), defaultPort, nil
}

func validatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%w: invalid port %q", ErrEgressDenied, port)
	}
	return nil
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	host = strings.TrimSuffix(host, ".")
	return strings.ToLower(host)
}
