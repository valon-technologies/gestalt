package bootstrap

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
)

type RuntimeHostServiceAccess string

const (
	RuntimeHostServiceAccessNone   RuntimeHostServiceAccess = "none"
	RuntimeHostServiceAccessRelay  RuntimeHostServiceAccess = "relay"
	RuntimeHostServiceAccessDirect RuntimeHostServiceAccess = "direct"
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
	RuntimeHostnameEgressDeliveryRuntime     RuntimeHostnameEgressDelivery = "runtime"
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
		HostServiceAccess: runtimeHostServiceAccessFromSupport(support.HostServiceAccess),
		EgressMode:        runtimeEgressModeFromSupport(support.EgressMode),
	}
}

func runtimeResolvedBehavior(advertised RuntimeBehavior, deps Deps) RuntimeBehavior {
	resolved := advertised
	if resolved.HostServiceAccess == RuntimeHostServiceAccessNone && hostCanRelayPluginRuntimeHostServices(deps) {
		resolved.HostServiceAccess = RuntimeHostServiceAccessRelay
	}
	if resolved.EgressMode == RuntimeEgressModeHostname && resolved.HostServiceAccess != RuntimeHostServiceAccessDirect && !hostCanProvideHostedHostnameEgress(deps) {
		resolved.EgressMode = RuntimeEgressModeNone
	}
	return resolved
}

func runtimeHostnameEgressDelivery(required bool, resolved RuntimeBehavior) RuntimeHostnameEgressDelivery {
	if !required || resolved.EgressMode != RuntimeEgressModeHostname {
		return RuntimeHostnameEgressDeliveryNone
	}
	if resolved.HostServiceAccess == RuntimeHostServiceAccessDirect {
		return RuntimeHostnameEgressDeliveryRuntime
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
	_, _, err := pluginRuntimePublicProxyBaseURL(deps.BaseURL)
	return err == nil
}

func hostCanProvideHostedHostnameEgress(deps Deps) bool {
	return hostCanRelayPluginRuntimeHostServices(deps)
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

func runtimeHostServiceAccessFromSupport(src pluginruntime.HostServiceAccess) RuntimeHostServiceAccess {
	switch src {
	case pluginruntime.HostServiceAccessDirect:
		return RuntimeHostServiceAccessDirect
	default:
		return RuntimeHostServiceAccessNone
	}
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
