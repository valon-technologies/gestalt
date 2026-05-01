package bootstrap

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
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
	CanHostPlugins    bool
	HostServiceAccess RuntimeHostServiceAccess
	EgressMode        RuntimeEgressMode
}

type HostedRuntimePlan struct {
	Resolved                  RuntimeBehavior
	RequiresHostServiceAccess bool
	RequiresHostnameEgress    bool
	HostnameEgressDelivery    RuntimeHostnameEgressDelivery
}

func buildHostedRuntimePlan(support pluginruntime.Support, deps Deps, requiresHostServiceAccess, requiresHostnameEgress bool) HostedRuntimePlan {
	resolved := runtimeResolvedBehavior(runtimeAdvertisedBehavior(support), deps)
	return HostedRuntimePlan{
		Resolved:                  resolved,
		RequiresHostServiceAccess: requiresHostServiceAccess,
		RequiresHostnameEgress:    requiresHostnameEgress,
		HostnameEgressDelivery:    runtimeHostnameEgressDelivery(requiresHostnameEgress, resolved),
	}
}

func buildPluginRuntimePlan(pluginName string, entry *config.ProviderEntry, deps Deps, support pluginruntime.Support) (HostedRuntimePlan, error) {
	requiresHostServiceAccess, requiresHostnameEgress, err := pluginRuntimeRequirementsForPlugin(pluginName, entry, deps)
	if err != nil {
		return HostedRuntimePlan{}, err
	}
	return buildHostedRuntimePlan(support, deps, requiresHostServiceAccess, requiresHostnameEgress), nil
}

func runtimeAdvertisedBehavior(support pluginruntime.Support) RuntimeBehavior {
	return RuntimeBehavior{
		CanHostPlugins:    support.CanHostPlugins,
		HostServiceAccess: RuntimeHostServiceAccessNone,
		EgressMode:        runtimeEgressModeFromSupport(support.EgressMode),
	}
}

func runtimeResolvedBehavior(advertised RuntimeBehavior, deps Deps) RuntimeBehavior {
	resolved := advertised
	if hostCanRelayPluginRuntimeHostServices(deps) {
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

func hostCanRelayPluginRuntimeHostServices(deps Deps) bool {
	if len(deps.EncryptionKey) == 0 {
		return false
	}
	baseURL, explicit := hostedRuntimeRelayBaseURL(deps)
	_, _, err := pluginRuntimePublicProxyBaseURL(baseURL, explicit)
	return err == nil
}

func hostCanProvideHostedHostnameEgress(deps Deps) bool {
	return hostCanRelayPluginRuntimeHostServices(deps)
}

func hostedRuntimeRelayBaseURL(deps Deps) (string, bool) {
	if baseURL := strings.TrimSpace(deps.RuntimeRelayBaseURL); baseURL != "" {
		return baseURL, true
	}
	return strings.TrimSpace(deps.BaseURL), false
}

func pluginRuntimeRequirementsForPlugin(name string, entry *config.ProviderEntry, deps Deps) (bool, bool, error) {
	if entry == nil {
		return false, false, nil
	}
	requiresHostServiceAccess := false
	effectiveIndexedDB, err := config.ResolveEffectivePluginIndexedDB(name, entry, deps.SelectedIndexedDBName, deps.IndexedDBDefs)
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

func (p HostedRuntimePlan) Validate(label string) error {
	if label == "" {
		label = "hosted runtime"
	}
	if !p.Resolved.CanHostPlugins {
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

func runtimeEgressModeFromSupport(src pluginruntime.EgressMode) RuntimeEgressMode {
	switch src {
	case pluginruntime.EgressModeHostname:
		return RuntimeEgressModeHostname
	case pluginruntime.EgressModeCIDR:
		return RuntimeEgressModeCIDR
	default:
		return RuntimeEgressModeNone
	}
}
