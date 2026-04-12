package egress

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// PolicyAction is the server-wide default for outbound requests.
type PolicyAction string

const (
	PolicyAllow PolicyAction = "allow"
	PolicyDeny  PolicyAction = "deny"
)

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
