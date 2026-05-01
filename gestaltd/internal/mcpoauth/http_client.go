package mcpoauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/services/egress"
)

type discoveryPolicy struct {
	allowInsecureLocal bool
}

func newHTTPClient(timeout time.Duration, policy discoveryPolicy) (*http.Client, func()) {
	transport := egress.CloneDefaultTransport()
	transport.Proxy = nil
	transport.DialContext = egress.SafeDialContext(destinationPolicy(policy))
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		if req == nil || req.URL == nil {
			return nil
		}
		if err := validateDiscoveryURL(req.Context(), req.URL.String(), policy); err != nil {
			return err
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	return &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: checkRedirect,
	}, transport.CloseIdleConnections
}

func destinationPolicy(policy discoveryPolicy) egress.DestinationPolicy {
	return egress.DestinationPolicy{AllowLoopback: policy.allowInsecureLocal}
}

func validateDiscoveryURL(ctx context.Context, rawURL string, policy discoveryPolicy) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parsing discovery URL: %w", err)
	}
	if u.Host == "" {
		return fmt.Errorf("discovery URL %q is missing host", rawURL)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !policy.allowInsecureLocal || !egress.IsLocalhostName(u.Hostname()) {
			return fmt.Errorf("discovery URL %q must use https", rawURL)
		}
	default:
		return fmt.Errorf("discovery URL %q must use https", rawURL)
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	if _, err := egress.ResolveSafeTCPAddr(ctx, "tcp", net.JoinHostPort(u.Hostname(), port), destinationPolicy(policy)); err != nil {
		return err
	}
	return nil
}
