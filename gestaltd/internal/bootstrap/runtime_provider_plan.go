package bootstrap

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
)

type RuntimeHostServiceAccess string

const (
	RuntimeHostServiceAccessNone   RuntimeHostServiceAccess = "none"
	RuntimeHostServiceAccessDirect RuntimeHostServiceAccess = "direct"
	RuntimeHostServiceAccessRelay  RuntimeHostServiceAccess = "relay"
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

type RuntimeBehavior struct {
	CanHostApps       bool
	HostServiceAccess RuntimeHostServiceAccess
	EgressMode        RuntimeEgressMode
}

type RuntimePlacementPlan struct {
	CanHostApps            bool
	HostServiceAccess      RuntimeHostServiceAccess
	EgressMode             RuntimeEgressMode
	RequiresHostnameEgress bool
	HostnameEgressDelivery RuntimeHostnameEgressDelivery
}

func buildRuntimePlacementPlan(support runtimeprovider.Support, deps Deps, requiresHostnameEgress bool) RuntimePlacementPlan {
	effective := runtimeEffectiveBehavior(support, deps)
	return RuntimePlacementPlan{
		CanHostApps:            effective.CanHostApps,
		HostServiceAccess:      effective.HostServiceAccess,
		EgressMode:             effective.EgressMode,
		RequiresHostnameEgress: requiresHostnameEgress,
		HostnameEgressDelivery: runtimeHostnameEgressDelivery(requiresHostnameEgress, effective),
	}
}

func buildRuntimePlan(entry *config.ProviderEntry, deps Deps, support runtimeprovider.Support) RuntimePlacementPlan {
	return buildRuntimePlacementPlan(support, deps, runtimeRequiresHostnameEgress(entry, deps))
}

func runtimeAdvertisedBehavior(support runtimeprovider.Support) RuntimeBehavior {
	return RuntimeBehavior{
		CanHostApps:       support.CanHostApps,
		HostServiceAccess: RuntimeHostServiceAccessNone,
		EgressMode:        runtimeEgressModeFromSupport(support.EgressMode),
	}
}

func runtimeEffectiveBehavior(support runtimeprovider.Support, deps Deps) RuntimeBehavior {
	return runtimeResolveBehavior(runtimeAdvertisedBehavior(support), runtimeHostServiceAccess(support, deps), deps)
}

func runtimeResolveBehavior(advertised RuntimeBehavior, hostServiceAccess RuntimeHostServiceAccess, deps Deps) RuntimeBehavior {
	resolved := advertised
	resolved.HostServiceAccess = hostServiceAccess
	if resolved.EgressMode == RuntimeEgressModeHostname && resolved.HostServiceAccess != RuntimeHostServiceAccessDirect && !hostCanProvideHostedHostnameEgress(deps) {
		resolved.EgressMode = RuntimeEgressModeNone
	}
	return resolved
}

func runtimeHostServiceAccess(support runtimeprovider.Support, deps Deps) RuntimeHostServiceAccess {
	if support.SupportsDirectHostServices {
		return RuntimeHostServiceAccessDirect
	}
	if hostCanRelayRuntimeHostServices(deps) {
		return RuntimeHostServiceAccessRelay
	}
	return RuntimeHostServiceAccessNone
}

func runtimeHostnameEgressDelivery(required bool, resolved RuntimeBehavior) RuntimeHostnameEgressDelivery {
	if !required || resolved.EgressMode != RuntimeEgressModeHostname {
		return RuntimeHostnameEgressDeliveryNone
	}
	if resolved.HostServiceAccess == RuntimeHostServiceAccessRelay {
		return RuntimeHostnameEgressDeliveryPublicProxy
	}
	return RuntimeHostnameEgressDeliveryNone
}

func hostCanRelayRuntimeHostServices(deps Deps) bool {
	if len(deps.EncryptionKey) == 0 {
		return false
	}
	baseURL, explicit := hostedRuntimeRelayBaseURL(deps)
	_, _, err := runtimePublicProxyBaseURL(baseURL, explicit)
	return err == nil
}

func hostCanProvideHostedHostnameEgress(deps Deps) bool {
	return hostCanRelayRuntimeHostServices(deps)
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

func (p RuntimePlacementPlan) Validate(label string) error {
	if label == "" {
		label = "hosted runtime"
	}
	if !p.CanHostApps {
		return fmt.Errorf("%s cannot host executable providers in a host-reachable session", label)
	}
	if p.HostServiceAccess == RuntimeHostServiceAccessNone {
		return fmt.Errorf("%s cannot provide host service access required by this provider", label)
	}
	if p.RequiresHostnameEgress && p.EgressMode != RuntimeEgressModeHostname {
		return fmt.Errorf("%s cannot preserve hostname-based egress required by this provider", label)
	}
	return nil
}

func runtimeEgressModeFromSupport(src runtimeprovider.EgressMode) RuntimeEgressMode {
	switch src {
	case runtimeprovider.EgressModeHostname:
		return RuntimeEgressModeHostname
	case runtimeprovider.EgressModeCIDR:
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
