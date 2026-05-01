package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerdev"
	"github.com/valon-technologies/gestalt/server/services/egress"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

func buildProviderDevManager(cfg *config.Config, providers *registry.ProviderMap[core.Provider], deps Deps) (*providerdev.Manager, error) {
	if cfg == nil || len(cfg.Plugins) == 0 {
		return nil, nil
	}

	sharedAttachmentState := cfg.Server.Dev.AttachmentState == config.DevAttachmentStateIndexedDB
	targets := make([]providerdev.Target, 0, len(cfg.Plugins))
	for name, entry := range cfg.Plugins {
		result := deriveProviderDevTarget(name, entry, providers, deps, sharedAttachmentState)
		switch result.state {
		case providerDevTargetAttachable:
			targets = append(targets, result.target)
		case providerDevTargetUnsupported:
			if result.err != nil {
				slog.Debug("skipping provider dev target", "provider", name, "reason", result.reason, "error", result.err)
			} else {
				slog.Debug("skipping provider dev target", "provider", name, "reason", result.reason)
			}
		case providerDevTargetInvalid:
			return nil, fmt.Errorf("provider dev target %q: %w", name, result.err)
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	var opts []providerdev.ManagerOption
	if sharedAttachmentState {
		if deps.Services == nil || deps.Services.DB == nil {
			return nil, fmt.Errorf("provider dev indexeddb attachment state requires core services IndexedDB")
		}
		opts = append(opts, providerdev.WithIndexedDBAttachmentState(context.Background(), deps.Services.DB))
	}
	manager, err := providerdev.NewManager(targets, opts...)
	if err != nil {
		return nil, err
	}
	if sharedAttachmentState {
		if err := registerProviderDevPublicHostServices(cfg, manager, deps, targets); err != nil {
			_ = manager.Close()
			return nil, err
		}
	}
	return manager, nil
}

type providerDevTargetState int

const (
	providerDevTargetUnsupported providerDevTargetState = iota
	providerDevTargetAttachable
	providerDevTargetInvalid
)

type providerDevTargetResult struct {
	state  providerDevTargetState
	target providerdev.Target
	reason string
	err    error
}

func deriveProviderDevTarget(name string, entry *config.ProviderEntry, providers *registry.ProviderMap[core.Provider], deps Deps, sharedAttachmentState bool) providerDevTargetResult {
	if entry == nil {
		return providerDevTargetResult{state: providerDevTargetUnsupported, reason: "missing config entry"}
	}
	if !entry.HasResolvedManifest() {
		return providerDevTargetResult{state: providerDevTargetUnsupported, reason: "manifest is not resolved"}
	}
	if providers == nil {
		if providerDevEntryIsLocal(entry) {
			return providerDevTargetResult{state: providerDevTargetInvalid, err: errors.New("provider registry is unavailable")}
		}
		return providerDevTargetResult{state: providerDevTargetUnsupported, reason: "provider registry is unavailable"}
	}
	provider, err := providers.Get(name)
	if err != nil {
		if providerDevEntryIsLocal(entry) {
			return providerDevTargetResult{state: providerDevTargetInvalid, err: fmt.Errorf("provider is not registered: %w", err)}
		}
		return providerDevTargetResult{state: providerDevTargetUnsupported, reason: "provider is not registered", err: err}
	}
	if provider == nil {
		if providerDevEntryIsLocal(entry) {
			return providerDevTargetResult{state: providerDevTargetInvalid, err: errors.New("provider is nil")}
		}
		return providerDevTargetResult{state: providerDevTargetUnsupported, reason: "provider is nil"}
	}
	pluginConfig, err := config.NodeToMap(entry.Config)
	if err != nil {
		if providerDevEntryIsLocal(entry) {
			return providerDevTargetResult{state: providerDevTargetInvalid, err: fmt.Errorf("config: %w", err)}
		}
		return providerDevTargetResult{state: providerDevTargetUnsupported, reason: "config cannot be converted", err: err}
	}

	targetName := name
	targetEntry := entry
	return providerDevTargetResult{
		state: providerDevTargetAttachable,
		target: providerdev.Target{
			Name:   targetName,
			Source: strings.TrimSpace(entry.ResolvedManifest.Source),
			Spec:   providerDevStaticSpecFromProvider(targetName, entry, provider),
			Config: pluginConfig,
			UIPath: strings.TrimSpace(entry.MountPath),
			RuntimeEnv: func(sessionID string) (providerdev.RuntimeEnv, error) {
				return buildProviderDevRuntimeEnv(targetName, targetEntry, deps, sessionID, sharedAttachmentState)
			},
		},
	}
}

func providerDevStaticSpecFromProvider(name string, entry *config.ProviderEntry, provider core.Provider) pluginservice.StaticProviderSpec {
	meta := resolveProviderMetadata(entry)
	spec := pluginservice.StaticProviderSpec{
		Name:             name,
		DisplayName:      provider.DisplayName(),
		Description:      provider.Description(),
		IconSVG:          meta.iconSVG,
		ConnectionMode:   provider.ConnectionMode(),
		AuthTypes:        slices.Clone(provider.AuthTypes()),
		ConnectionParams: maps.Clone(provider.ConnectionParamDefs()),
		CredentialFields: slices.Clone(provider.CredentialFields()),
		DiscoveryConfig:  cloneProviderDevDiscoveryConfig(provider.DiscoveryConfig()),
	}
	if spec.DisplayName == "" {
		spec.DisplayName = name
	}
	if cat := provider.Catalog(); cat != nil {
		if spec.IconSVG == "" {
			spec.IconSVG = cat.IconSVG
		}
		spec.Catalog = cat.Clone()
	}
	return spec
}

func cloneProviderDevDiscoveryConfig(discovery *core.DiscoveryConfig) *core.DiscoveryConfig {
	if discovery == nil {
		return nil
	}
	out := *discovery
	out.Metadata = maps.Clone(discovery.Metadata)
	return &out
}

func providerDevEntryIsLocal(entry *config.ProviderEntry) bool {
	return entry != nil && (entry.HasLocalSource() || entry.HasLocalReleaseSource())
}

func buildProviderDevRuntimeEnv(name string, entry *config.ProviderEntry, deps Deps, sessionID string, sharedAttachmentState bool) (providerdev.RuntimeEnv, error) {
	hostServices, _, cleanup, err := buildPluginRuntimeHostServices(name, entry, deps, false)
	if err != nil {
		return providerdev.RuntimeEnv{}, err
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError && cleanup != nil {
			cleanup()
		}
	}()
	env := withRuntimeSessionEnv(map[string]string{}, sessionID)
	if sharedAttachmentState {
		for _, hostService := range hostServices {
			bindingReq, bindingEnv, _, err := buildHostedRuntimeHostServiceBinding(name, sessionID, hostServiceBindingDescriptorFromConfigured(hostService), deps, false, false)
			if err != nil {
				return providerdev.RuntimeEnv{}, err
			}
			if bindingReq.EnvVar != "" && bindingReq.Relay.DialTarget != "" {
				env[bindingReq.EnvVar] = bindingReq.Relay.DialTarget
			}
			maps.Copy(env, bindingEnv)
		}
		if deps.Egress.DefaultAction == egress.PolicyDeny {
			proxyEnv, err := buildHostedRuntimePublicEgressProxy(name, sessionID, entry.EffectiveAllowedHosts(), deps.Egress.DefaultAction, deps)
			if err != nil {
				return providerdev.RuntimeEnv{}, err
			}
			maps.Copy(env, proxyEnv)
		}
		if cleanup != nil {
			cleanup()
			cleanup = nil
		}
		cleanupOnError = false
		return providerdev.RuntimeEnv{Env: env}, nil
	}
	if deps.PublicHostServices != nil {
		deps.PublicHostServices.RegisterSession(name, sessionID, hostServices...)
		cleanup = chainCleanup(cleanup, func() {
			deps.PublicHostServices.UnregisterSession(name, sessionID, hostServices...)
		})
	}
	startedHostServices, err := runtimehost.StartHostServices(
		hostServices,
		runtimehost.WithHostServicesProviderName(name),
		runtimehost.WithHostServicesTelemetry(deps.Telemetry),
	)
	if err != nil {
		return providerdev.RuntimeEnv{}, fmt.Errorf("start host services: %w", err)
	}
	if startedHostServices != nil {
		cleanup = chainCleanup(cleanup, func() {
			_ = startedHostServices.Close()
		})
	}
	for _, hostService := range startedHostServices.Bindings() {
		bindingReq, bindingEnv, _, err := buildHostedRuntimeHostServiceBinding(name, sessionID, hostServiceBindingDescriptorFromStarted(hostService), deps, false, false)
		if err != nil {
			return providerdev.RuntimeEnv{}, err
		}
		if bindingReq.EnvVar != "" && bindingReq.Relay.DialTarget != "" {
			env[bindingReq.EnvVar] = bindingReq.Relay.DialTarget
		}
		maps.Copy(env, bindingEnv)
	}
	if deps.Egress.DefaultAction == egress.PolicyDeny {
		proxyEnv, err := buildHostedRuntimePublicEgressProxy(name, sessionID, entry.EffectiveAllowedHosts(), deps.Egress.DefaultAction, deps)
		if err != nil {
			return providerdev.RuntimeEnv{}, err
		}
		maps.Copy(env, proxyEnv)
	}

	cleanupOnError = false
	return providerdev.RuntimeEnv{
		Env:     env,
		Cleanup: cleanup,
	}, nil
}

type providerDevHostServiceVerifier struct {
	manager  *providerdev.Manager
	provider string
}

func (v providerDevHostServiceVerifier) VerifyHostServiceSession(ctx context.Context, sessionID string) error {
	if v.manager == nil {
		return fmt.Errorf("provider dev manager is not configured")
	}
	return v.manager.VerifyHostServiceSession(ctx, v.provider, sessionID)
}

func registerProviderDevPublicHostServices(cfg *config.Config, manager *providerdev.Manager, deps Deps, targets []providerdev.Target) error {
	if cfg == nil || manager == nil || deps.PublicHostServices == nil {
		return nil
	}
	targetNames := make(map[string]struct{}, len(targets))
	for i := range targets {
		targetNames[targets[i].Name] = struct{}{}
	}
	for name, entry := range cfg.Plugins {
		if _, ok := targetNames[name]; !ok {
			continue
		}
		if entry == nil || !entry.HasResolvedManifest() {
			continue
		}
		hostServices, _, cleanup, err := buildPluginRuntimeHostServices(name, entry, deps, false)
		if err != nil {
			if cleanup != nil {
				cleanup()
			}
			return fmt.Errorf("provider dev public host services %q: %w", name, err)
		}
		if len(hostServices) == 0 {
			if cleanup != nil {
				cleanup()
			}
			continue
		}
		deps.PublicHostServices.RegisterVerified(name, providerDevHostServiceVerifier{manager: manager, provider: name}, hostServices...)
		manager.AddCleanup(func() {
			deps.PublicHostServices.Unregister(name, hostServices...)
			if cleanup != nil {
				cleanup()
			}
		})
	}
	return nil
}
