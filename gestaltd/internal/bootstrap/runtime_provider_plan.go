package bootstrap

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type RuntimeEgressMode string

const (
	RuntimeEgressModeNone     RuntimeEgressMode = "none"
	RuntimeEgressModeCIDR     RuntimeEgressMode = "cidr"
	RuntimeEgressModeHostname RuntimeEgressMode = "hostname"
)

type RuntimeHostnameEgressDelivery string

const (
	RuntimeHostnameEgressDeliveryNone        RuntimeHostnameEgressDelivery = "none"
	RuntimeHostnameEgressDeliveryPublicProxy RuntimeHostnameEgressDelivery = "public_proxy"
)

type RuntimePlacementPlan struct {
	CanHostApps            bool
	EgressMode             RuntimeEgressMode
	RequiresHostnameEgress bool
	HostnameEgressDelivery RuntimeHostnameEgressDelivery
}

func buildRuntimePlacementPlan(support *proto.RuntimeSupport, deps Deps, requiresHostnameEgress bool) RuntimePlacementPlan {
	relayAvailable := hostCanRelayRuntimeHostServices(deps)
	egressMode := runtimeEgressModeFromSupport(support.GetEgressMode())
	if egressMode == RuntimeEgressModeHostname && !relayAvailable {
		egressMode = RuntimeEgressModeNone
	}
	return RuntimePlacementPlan{
		CanHostApps:            support.GetCanHostApps(),
		EgressMode:             egressMode,
		RequiresHostnameEgress: requiresHostnameEgress,
		HostnameEgressDelivery: runtimeHostnameEgressDelivery(requiresHostnameEgress, relayAvailable, egressMode),
	}
}

func buildRuntimePlan(entry *config.ProviderEntry, deps Deps, support *proto.RuntimeSupport) RuntimePlacementPlan {
	return buildRuntimePlacementPlan(support, deps, runtimeRequiresHostnameEgress(entry, deps))
}

func runtimeHostnameEgressDelivery(required bool, relayAvailable bool, egressMode RuntimeEgressMode) RuntimeHostnameEgressDelivery {
	if !required || !relayAvailable || egressMode != RuntimeEgressModeHostname {
		return RuntimeHostnameEgressDeliveryNone
	}
	return RuntimeHostnameEgressDeliveryPublicProxy
}

func hostCanRelayRuntimeHostServices(deps Deps) bool {
	if len(deps.EncryptionKey) == 0 {
		return false
	}
	baseURL, explicit := hostedRuntimeRelayBaseURL(deps)
	_, _, err := runtimePublicProxyBaseURL(baseURL, explicit)
	return err == nil
}

func hostedRuntimeRelayBaseURL(deps Deps) (string, bool) {
	if baseURL := strings.TrimSpace(deps.RuntimeRelayBaseURL); baseURL != "" {
		return baseURL, true
	}
	return strings.TrimSpace(deps.BaseURL), false
}

func runtimeRequiresHostnameEgress(entry *config.ProviderEntry, deps Deps) bool {
	if entry == nil {
		return false
	}
	return deps.Egress.ProviderPolicy(entry).RequiresHostnameEnforcement()
}

func (p RuntimePlacementPlan) Validate(label string, deps Deps) error {
	if label == "" {
		label = "hosted runtime"
	}
	if !p.CanHostApps {
		return fmt.Errorf("%s cannot host executable providers in a host-reachable session", label)
	}
	if !hostCanRelayRuntimeHostServices(deps) {
		return fmt.Errorf("%s cannot provide host service access required by this provider", label)
	}
	if p.RequiresHostnameEgress && p.EgressMode != RuntimeEgressModeHostname {
		return fmt.Errorf("%s cannot preserve hostname-based egress required by this provider", label)
	}
	return nil
}

func runtimeEgressModeFromSupport(src proto.RuntimeEgressMode) RuntimeEgressMode {
	switch src {
	case proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME:
		return RuntimeEgressModeHostname
	case proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_CIDR:
		return RuntimeEgressModeCIDR
	default:
		return RuntimeEgressModeNone
	}
}

func runtimePublicRelayTarget(baseURL string, allowInsecureHTTP bool) (string, string, error) {
	parsed, host, err := runtimePublicProxyBaseURL(baseURL, allowInsecureHTTP)
	if err != nil {
		return "", "", err
	}
	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(parsed.Scheme, "http") {
			port = "80"
		} else {
			port = "443"
		}
	}
	target := net.JoinHostPort(host, port)
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return "tls://" + target, host, nil
	case "http":
		return "tcp://" + target, host, nil
	default:
		return "", "", fmt.Errorf("server.baseURL %q has unsupported public runtime relay scheme %q", baseURL, parsed.Scheme)
	}
}

func runtimePublicProxyBaseURL(baseURL string, allowInsecureHTTP bool) (*url.URL, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, "", fmt.Errorf("parse server.baseURL for public runtime relay: %w", err)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return nil, "", fmt.Errorf("server.baseURL %q is missing a hostname", baseURL)
	}
	if path := strings.TrimSpace(parsed.EscapedPath()); path != "" && path != "/" {
		return nil, "", fmt.Errorf("server.baseURL %q must not include a path for public runtime relay", baseURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, "", fmt.Errorf("server.baseURL %q must not include a query or fragment for public runtime relay", baseURL)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		if !allowInsecureHTTP && !isLoopbackAllowedHost(host) {
			return nil, "", fmt.Errorf("server.baseURL %q must use https for public runtime relay unless it targets loopback", baseURL)
		}
	default:
		return nil, "", fmt.Errorf("server.baseURL %q must use https for public runtime relay", baseURL)
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, host, nil
}

func isLoopbackAllowedHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
