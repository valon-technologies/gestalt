package egress

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// PrivateNetworkPolicy controls whether outbound HTTP connections may reach
// private/internal IP addresses. When AllowPrivateNetworks is false (the
// default), connections to loopback, RFC 1918, link-local, and unspecified
// addresses are rejected at dial time -- after DNS resolution -- to prevent
// SSRF and DNS rebinding attacks.
type PrivateNetworkPolicy struct {
	AllowPrivateNetworks bool
}

const dialTimeout = 10 * time.Second

func (p *PrivateNetworkPolicy) checkAddr(ip net.IP) error {
	if p == nil || p.AllowPrivateNetworks {
		return nil
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("%w: connection to private address %s is not allowed", ErrEgressDenied, ip)
	}
	return nil
}

// SafeDialContext returns a DialContext function suitable for http.Transport
// that validates resolved IP addresses against the policy before connecting.
func SafeDialContext(policy *PrivateNetworkPolicy) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("splitting address %q: %w", addr, err)
		}

		// If the host is already an IP literal, check it directly.
		if ip := net.ParseIP(host); ip != nil {
			if err := policy.checkAddr(ip); err != nil {
				return nil, err
			}
			return (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, network, addr)
		}

		// Resolve hostname and validate all returned IPs.
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolving %q: %w", host, err)
		}

		var lastErr error
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip == nil {
				continue
			}
			if err := policy.checkAddr(ip); err != nil {
				lastErr = err
				continue
			}
			conn, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, network, net.JoinHostPort(ipStr, port))
			if err != nil {
				lastErr = err
				continue
			}
			return conn, nil
		}

		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no usable addresses for %q", host)
	}
}

// SafeTransport returns an *http.Transport that validates resolved IPs against
// the given policy before establishing connections. If policy is nil, private
// networks are allowed (no filtering).
func SafeTransport(policy *PrivateNetworkPolicy) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if policy != nil && !policy.AllowPrivateNetworks {
		t.DialContext = SafeDialContext(policy)
	}
	return t
}

// SafeClient returns an *http.Client whose transport validates resolved IPs
// against the given policy. If policy is nil, private networks are allowed.
func SafeClient(policy *PrivateNetworkPolicy, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: SafeTransport(policy),
		Timeout:   timeout,
	}
}
