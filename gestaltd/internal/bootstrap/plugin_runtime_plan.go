package bootstrap

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
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

type RuntimeLaunchMode string

const (
	RuntimeLaunchModeHostPath RuntimeLaunchMode = "host_path"
	RuntimeLaunchModeBundle   RuntimeLaunchMode = "bundle"
)

type RuntimeExecutionTarget struct {
	GOOS   string
	GOARCH string
}

func (t RuntimeExecutionTarget) IsSet() bool {
	return strings.TrimSpace(t.GOOS) != "" && strings.TrimSpace(t.GOARCH) != ""
}

type RuntimeBehavior struct {
	CanHostPlugins    bool
	HostServiceAccess RuntimeHostServiceAccess
	EgressMode        RuntimeEgressMode
	LaunchMode        RuntimeLaunchMode
	ExecutionTarget   RuntimeExecutionTarget
}

type PluginRuntimePlan struct {
	Resolved                  RuntimeBehavior
	RequiresHostServiceAccess bool
	RequiresHostnameEgress    bool
	HostnameEgressDelivery    RuntimeHostnameEgressDelivery
}

func buildPluginRuntimePlan(pluginName string, entry *config.ProviderEntry, deps Deps, support pluginruntime.Support) (PluginRuntimePlan, error) {
	advertised := runtimeAdvertisedBehavior(support)
	resolved := runtimeResolvedBehavior(advertised, deps)
	requiresHostServiceAccess, requiresHostnameEgress, err := pluginRuntimeRequirementsForPlugin(pluginName, entry, deps)
	if err != nil {
		return PluginRuntimePlan{}, err
	}
	return PluginRuntimePlan{
		Resolved:                  resolved,
		RequiresHostServiceAccess: requiresHostServiceAccess,
		RequiresHostnameEgress:    requiresHostnameEgress,
		HostnameEgressDelivery:    runtimeHostnameEgressDelivery(requiresHostnameEgress, resolved),
	}, nil
}

func runtimeAdvertisedBehavior(support pluginruntime.Support) RuntimeBehavior {
	return RuntimeBehavior{
		CanHostPlugins:    support.CanHostPlugins,
		HostServiceAccess: runtimeHostServiceAccessFromSupport(support.HostServiceAccess),
		EgressMode:        runtimeEgressModeFromSupport(support.EgressMode),
		LaunchMode:        runtimeLaunchModeFromSupport(support.LaunchMode),
		ExecutionTarget: RuntimeExecutionTarget{
			GOOS:   strings.TrimSpace(support.ExecutionTarget.GOOS),
			GOARCH: strings.TrimSpace(support.ExecutionTarget.GOARCH),
		},
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
	return requiresHostServiceAccess, len(entry.AllowedHosts) > 0 || deps.Egress.DefaultAction == egress.PolicyDeny, nil
}

func (p PluginRuntimePlan) Validate(label string) error {
	if label == "" {
		label = "plugin runtime"
	}
	if !p.Resolved.CanHostPlugins {
		return fmt.Errorf("%s cannot host executable plugins in a host-reachable session", label)
	}
	if p.RequiresHostServiceAccess && p.Resolved.HostServiceAccess == RuntimeHostServiceAccessNone {
		return fmt.Errorf("%s cannot provide host service access required by this plugin", label)
	}
	if p.RequiresHostnameEgress && p.Resolved.EgressMode != RuntimeEgressModeHostname {
		return fmt.Errorf("%s cannot preserve hostname-based egress required by this plugin", label)
	}
	if p.Resolved.LaunchMode == RuntimeLaunchModeBundle && !p.Resolved.ExecutionTarget.IsSet() {
		return fmt.Errorf("%s cannot stage hosted plugin bundles because it does not declare an execution target", label)
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

func runtimeLaunchModeFromSupport(src pluginruntime.LaunchMode) RuntimeLaunchMode {
	switch src {
	case pluginruntime.LaunchModeHostPath:
		return RuntimeLaunchModeHostPath
	default:
		return RuntimeLaunchModeBundle
	}
}
