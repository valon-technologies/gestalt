package bootstrap

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
)

type RuntimeHostServiceMode string

const (
	RuntimeHostServiceModeNone   RuntimeHostServiceMode = "none"
	RuntimeHostServiceModeRelay  RuntimeHostServiceMode = "relay"
	RuntimeHostServiceModeDirect RuntimeHostServiceMode = "direct"
)

type RuntimeEgressMode string

const (
	RuntimeEgressModeNone     RuntimeEgressMode = "none"
	RuntimeEgressModeCIDR     RuntimeEgressMode = "cidr"
	RuntimeEgressModeHostname RuntimeEgressMode = "hostname"
)

type RuntimeHostnameEgressTransport string

const (
	RuntimeHostnameEgressTransportNone        RuntimeHostnameEgressTransport = "none"
	RuntimeHostnameEgressTransportRuntime     RuntimeHostnameEgressTransport = "runtime"
	RuntimeHostnameEgressTransportPublicProxy RuntimeHostnameEgressTransport = "public_proxy"
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

type RuntimeProfile struct {
	HostedExecution bool
	HostServiceMode RuntimeHostServiceMode
	EgressMode      RuntimeEgressMode
	LaunchMode      RuntimeLaunchMode
	ExecutionTarget RuntimeExecutionTarget
}

type PluginRuntimePlan struct {
	Effective                 RuntimeProfile
	RequiresHostServiceAccess bool
	RequiresHostnameEgress    bool
	HostnameEgressTransport   RuntimeHostnameEgressTransport
}

func buildPluginRuntimePlan(pluginName string, entry *config.ProviderEntry, deps Deps, caps pluginruntime.Capabilities) (PluginRuntimePlan, error) {
	advertised := runtimeAdvertisedProfile(caps)
	effective := runtimeEffectiveProfile(advertised, caps, deps)
	requiresHostServiceAccess, requiresHostnameEgress, err := pluginRuntimeRequirementsForPlugin(pluginName, entry, deps)
	if err != nil {
		return PluginRuntimePlan{}, err
	}
	return PluginRuntimePlan{
		Effective:                 effective,
		RequiresHostServiceAccess: requiresHostServiceAccess,
		RequiresHostnameEgress:    requiresHostnameEgress,
		HostnameEgressTransport:   runtimeHostnameEgressTransport(requiresHostnameEgress, caps, effective, deps),
	}, nil
}

func runtimeAdvertisedProfile(caps pluginruntime.Capabilities) RuntimeProfile {
	hostServiceMode := RuntimeHostServiceModeNone
	if caps.HostServiceTunnels {
		hostServiceMode = RuntimeHostServiceModeDirect
	}
	egressMode := RuntimeEgressModeNone
	switch {
	case caps.HostnameProxyEgress:
		egressMode = RuntimeEgressModeHostname
	case caps.CIDREgress:
		egressMode = RuntimeEgressModeCIDR
	}
	launchMode := RuntimeLaunchModeBundle
	if caps.HostPathExecution {
		launchMode = RuntimeLaunchModeHostPath
	}
	return RuntimeProfile{
		HostedExecution: caps.HostedPluginRuntime && caps.ProviderGRPCTunnel,
		HostServiceMode: hostServiceMode,
		EgressMode:      egressMode,
		LaunchMode:      launchMode,
		ExecutionTarget: RuntimeExecutionTarget{
			GOOS:   strings.TrimSpace(caps.ExecutionGOOS),
			GOARCH: strings.TrimSpace(caps.ExecutionGOARCH),
		},
	}
}

func runtimeEffectiveProfile(advertised RuntimeProfile, caps pluginruntime.Capabilities, deps Deps) RuntimeProfile {
	effective := advertised
	if effective.HostServiceMode == RuntimeHostServiceModeNone && hostCanRelayPluginRuntimeHostServices(deps) {
		effective.HostServiceMode = RuntimeHostServiceModeRelay
	}
	if effective.EgressMode == RuntimeEgressModeHostname && !caps.HostServiceTunnels && !hostCanProvideHostedHostnameEgress(deps) {
		effective.EgressMode = RuntimeEgressModeNone
	}
	return effective
}

func runtimeHostnameEgressTransport(required bool, caps pluginruntime.Capabilities, effective RuntimeProfile, deps Deps) RuntimeHostnameEgressTransport {
	if !required || effective.EgressMode != RuntimeEgressModeHostname {
		return RuntimeHostnameEgressTransportNone
	}
	if caps.HostServiceTunnels {
		return RuntimeHostnameEgressTransportRuntime
	}
	if hostCanProvideHostedHostnameEgress(deps) {
		return RuntimeHostnameEgressTransportPublicProxy
	}
	return RuntimeHostnameEgressTransportNone
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
	if !p.Effective.HostedExecution {
		return fmt.Errorf("%s cannot host executable plugins in a host-reachable session", label)
	}
	if p.RequiresHostServiceAccess && p.Effective.HostServiceMode == RuntimeHostServiceModeNone {
		return fmt.Errorf("%s cannot provide host service access required by this plugin", label)
	}
	if p.RequiresHostnameEgress && p.Effective.EgressMode != RuntimeEgressModeHostname {
		return fmt.Errorf("%s cannot preserve hostname-based egress required by this plugin", label)
	}
	if p.Effective.LaunchMode == RuntimeLaunchModeBundle && !p.Effective.ExecutionTarget.IsSet() {
		return fmt.Errorf("%s cannot stage hosted plugin bundles because it does not declare an execution target", label)
	}
	return nil
}
