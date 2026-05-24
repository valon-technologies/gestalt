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
	RuntimeHostServiceAccessNone  RuntimeHostServiceAccess = "none"
	RuntimeHostServiceAccessRelay RuntimeHostServiceAccess = "relay"
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
	Resolved                  RuntimeBehavior
	RequiresHostServiceAccess bool
	RequiresHostnameEgress    bool
	HostnameEgressDelivery    RuntimeHostnameEgressDelivery
}

func buildRuntimePlacementPlan(support runtimeprovider.Support, deps Deps, requiresHostServiceAccess, requiresHostnameEgress bool) RuntimePlacementPlan {
	resolved := runtimeResolvedBehavior(runtimeAdvertisedBehavior(support), deps)
	return RuntimePlacementPlan{
		Resolved:                  resolved,
		RequiresHostServiceAccess: requiresHostServiceAccess,
		RequiresHostnameEgress:    requiresHostnameEgress,
		HostnameEgressDelivery:    runtimeHostnameEgressDelivery(requiresHostnameEgress, resolved),
	}
}

func buildRuntimePlan(pluginName string, entry *config.ProviderEntry, deps Deps, support runtimeprovider.Support) (RuntimePlacementPlan, error) {
	requiresHostServiceAccess, requiresHostnameEgress, err := runtimeRequirementsForApp(pluginName, entry, deps)
	if err != nil {
		return RuntimePlacementPlan{}, err
	}
	return buildRuntimePlacementPlan(support, deps, requiresHostServiceAccess, requiresHostnameEgress), nil
}

func runtimeAdvertisedBehavior(support runtimeprovider.Support) RuntimeBehavior {
	return RuntimeBehavior{
		CanHostApps:       support.CanHostApps,
		HostServiceAccess: RuntimeHostServiceAccessNone,
		EgressMode:        runtimeEgressModeFromSupport(support.EgressMode),
	}
}

func runtimeResolvedBehavior(advertised RuntimeBehavior, deps Deps) RuntimeBehavior {
	resolved := advertised
	if hostCanRelayRuntimeHostServices(deps) {
		resolved.HostServiceAccess = RuntimeHostServiceAccessRelay
	} else {
		resolved.HostServiceAccess = RuntimeHostServiceAccessNone
	}
	if resolved.EgressMode == RuntimeEgressModeHostname && !hostCanProvideHostedHostnameEgress(deps) {
		resolved.EgressMode = RuntimeEgressModeNone
	}
	return resolved
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

func runtimeRequirementsForApp(name string, entry *config.ProviderEntry, deps Deps) (bool, bool, error) {
	if entry == nil {
		return false, false, nil
	}
	requiresHostServiceAccess := false
	effectiveIndexedDB, err := config.ResolveEffectiveAppIndexedDB(name, entry, deps.SelectedIndexedDBName, deps.IndexedDBDefs)
	if err != nil {
		return false, false, err
	}
	if effectiveIndexedDB.Enabled {
		requiresHostServiceAccess = true
	}
	if len(entry.Cache) > 0 {
		requiresHostServiceAccess = true
	}
	if len(entry.S3) > 0 {
		requiresHostServiceAccess = true
	}
	if deps.WorkflowManager != nil || (deps.WorkflowRuntime != nil && deps.WorkflowRuntime.HasConfiguredProviders()) {
		requiresHostServiceAccess = true
	}
	if deps.AuthorizationProvider != nil && len(entry.EffectiveHTTPBindings()) > 0 {
		requiresHostServiceAccess = true
	}
	if len(entry.Invokes) > 0 {
		requiresHostServiceAccess = true
	}
	return requiresHostServiceAccess, deps.Egress.ProviderPolicy(entry).RequiresHostnameEnforcement(), nil
}

func agentRuntimeRequirementsForProvider(name string, entry *config.ProviderEntry, deps Deps) (bool, bool, error) {
	if entry == nil {
		return false, false, nil
	}
	if _, err := config.ResolveEffectiveAgentIndexedDB(name, entry, deps.IndexedDBDefs); err != nil {
		return false, false, err
	}
	return true, deps.Egress.ProviderPolicy(entry).RequiresHostnameEnforcement(), nil
}

func (p RuntimePlacementPlan) Validate(label string) error {
	if label == "" {
		label = "hosted runtime"
	}
	if !p.Resolved.CanHostApps {
		return fmt.Errorf("%s cannot host executable providers in a host-reachable session", label)
	}
	if p.RequiresHostServiceAccess && p.Resolved.HostServiceAccess == RuntimeHostServiceAccessNone {
		return fmt.Errorf("%s cannot provide host service access required by this provider", label)
	}
	if p.RequiresHostnameEgress && p.Resolved.EgressMode != RuntimeEgressModeHostname {
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
