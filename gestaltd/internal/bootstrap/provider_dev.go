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
	"github.com/valon-technologies/gestalt/server/services/egress"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
)

func buildProviderDevManager(cfg *config.Config, providers *registry.ProviderMap[core.Provider], deps Deps) (*providerdev.Manager, error) {
	if cfg == nil || len(cfg.Plugins) == 0 {
		return nil, nil
	}

	sharedAttachmentState := cfg.Server.Dev.AttachmentState == config.DevAttachmentStateIndexedDB
	runtimeHostServiceDescriptors := map[string][]hostServiceBindingDescriptor{}
	targets := make([]providerdev.Target, 0, len(cfg.Plugins))
	for name, entry := range cfg.Plugins {
		result := deriveProviderDevTarget(name, entry, providers, deps, runtimeHostServiceDescriptors)
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
	if err := registerProviderDevPublicHostServices(cfg, manager, deps, targets, runtimeHostServiceDescriptors); err != nil {
		_ = manager.Close()
		return nil, err
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

func deriveProviderDevTarget(name string, entry *config.ProviderEntry, providers *registry.ProviderMap[core.Provider], deps Deps, runtimeHostServiceDescriptors map[string][]hostServiceBindingDescriptor) providerDevTargetResult {
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
				return buildProviderDevRuntimeEnv(targetName, targetEntry, deps, sessionID, runtimeHostServiceDescriptors[targetName])
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

func buildProviderDevRuntimeEnv(name string, entry *config.ProviderEntry, deps Deps, sessionID string, hostServices []hostServiceBindingDescriptor) (providerdev.RuntimeEnv, error) {
	env := withRuntimeSessionEnv(map[string]string{}, sessionID)
	env = withHostServiceTLSCAEnv(env, deps)
	for _, hostService := range hostServices {
		bindingEnv, _, err := buildHostedRuntimeHostServiceEnv(name, sessionID, hostService, deps)
		if err != nil {
			return providerdev.RuntimeEnv{}, err
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

	return providerdev.RuntimeEnv{Env: env}, nil
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

func registerProviderDevPublicHostServices(cfg *config.Config, manager *providerdev.Manager, deps Deps, targets []providerdev.Target, runtimeHostServiceDescriptors map[string][]hostServiceBindingDescriptor) error {
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
		hostServices, _, cleanup, err := buildPluginRuntimeHostServices(name, entry, deps)
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
		if runtimeHostServiceDescriptors != nil {
			runtimeHostServiceDescriptors[name] = hostServiceBindingDescriptorsFromConfigured(hostServices)
		}
		registration := deps.PublicHostServices.RegisterVerified(name, providerDevHostServiceVerifier{manager: manager, provider: name}, hostServices...)
		manager.AddCleanup(func() {
			registration.Unregister()
			if cleanup != nil {
				cleanup()
			}
		})
	}
	return nil
}
