package bootstrap

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/appruntime"
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
	CanHostApps    bool
	HostServiceAccess RuntimeHostServiceAccess
	EgressMode        RuntimeEgressMode
}

type RuntimePlacementPlan struct {
	Resolved                  RuntimeBehavior
	RequiresHostServiceAccess bool
	RequiresHostnameEgress    bool
	HostnameEgressDelivery    RuntimeHostnameEgressDelivery
}

func buildRuntimePlacementPlan(support appruntime.Support, deps Deps, requiresHostServiceAccess, requiresHostnameEgress bool) RuntimePlacementPlan {
	resolved := runtimeResolvedBehavior(runtimeAdvertisedBehavior(support), deps)
	return RuntimePlacementPlan{
		Resolved:                  resolved,
		RequiresHostServiceAccess: requiresHostServiceAccess,
		RequiresHostnameEgress:    requiresHostnameEgress,
		HostnameEgressDelivery:    runtimeHostnameEgressDelivery(requiresHostnameEgress, resolved),
	}
}

func buildAppRuntimePlan(pluginName string, entry *config.ProviderEntry, deps Deps, support appruntime.Support) (RuntimePlacementPlan, error) {
	requiresHostServiceAccess, requiresHostnameEgress, err := pluginRuntimeRequirementsForPlugin(pluginName, entry, deps)
	if err != nil {
		return RuntimePlacementPlan{}, err
	}
	return buildRuntimePlacementPlan(support, deps, requiresHostServiceAccess, requiresHostnameEgress), nil
}

func runtimeAdvertisedBehavior(support appruntime.Support) RuntimeBehavior {
	return RuntimeBehavior{
		CanHostApps:    support.CanHostApps,
		HostServiceAccess: RuntimeHostServiceAccessNone,
		EgressMode:        runtimeEgressModeFromSupport(support.EgressMode),
	}
}

func runtimeResolvedBehavior(advertised RuntimeBehavior, deps Deps) RuntimeBehavior {
	resolved := advertised
	if hostCanRelayAppRuntimeHostServices(deps) {
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

func hostCanRelayAppRuntimeHostServices(deps Deps) bool {
	if len(deps.EncryptionKey) == 0 {
		return false
	}
	baseURL, explicit := hostedRuntimeRelayBaseURL(deps)
	_, _, err := pluginRuntimePublicProxyBaseURL(baseURL, explicit)
	return err == nil
}

func hostCanProvideHostedHostnameEgress(deps Deps) bool {
	return hostCanRelayAppRuntimeHostServices(deps)
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

func runtimeEgressModeFromSupport(src appruntime.EgressMode) RuntimeEgressMode {
	switch src {
	case appruntime.EgressModeHostname:
		return RuntimeEgressModeHostname
	case appruntime.EgressModeCIDR:
		return RuntimeEgressModeCIDR
	default:
		return RuntimeEgressModeNone
	}
}
