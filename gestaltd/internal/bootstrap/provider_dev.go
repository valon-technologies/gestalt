package bootstrap

import (
	"fmt"
	"maps"
	"slices"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/providerdev"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

func buildProviderDevManager(cfg *config.Config, providers *registry.ProviderMap[core.Provider], deps Deps) (*providerdev.Manager, error) {
	if cfg == nil || len(cfg.Plugins) == 0 {
		return nil, nil
	}

	targets := make([]providerdev.Target, 0, len(cfg.Plugins))
	for name, entry := range cfg.Plugins {
		if entry == nil || !entry.HasResolvedManifest() {
			continue
		}
		if _, err := providers.Get(name); err != nil {
			continue
		}
		spec, _, err := buildStartupProviderSpec(name, entry)
		if err != nil {
			return nil, fmt.Errorf("provider dev target %q: %w", name, err)
		}
		pluginConfig, err := config.NodeToMap(entry.Config)
		if err != nil {
			return nil, fmt.Errorf("provider dev target %q config: %w", name, err)
		}
		targetName := name
		targetEntry := entry
		targets = append(targets, providerdev.Target{
			Name:   targetName,
			Spec:   spec,
			Config: pluginConfig,
			RuntimeEnv: func(sessionID string) (providerdev.RuntimeEnv, error) {
				return buildProviderDevRuntimeEnv(targetName, targetEntry, deps, sessionID)
			},
		})
	}
	if len(targets) == 0 {
		return nil, nil
	}
	return providerdev.NewManager(targets)
}

func buildProviderDevRuntimeEnv(name string, entry *config.ProviderEntry, deps Deps, sessionID string) (providerdev.RuntimeEnv, error) {
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

	env := map[string]string{}
	var allowedHosts []string
	startedHostServices, err := providerhost.StartHostServices(
		hostServices,
		providerhost.WithHostServicesProviderName(name),
		providerhost.WithHostServicesTelemetry(deps.Telemetry),
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
		bindingReq, bindingEnv, relayHost, err := buildHostedRuntimeHostServiceBinding(name, sessionID, hostService, deps, false)
		if err != nil {
			return providerdev.RuntimeEnv{}, err
		}
		if bindingReq.EnvVar != "" && bindingReq.Relay.DialTarget != "" {
			env[bindingReq.EnvVar] = bindingReq.Relay.DialTarget
		}
		maps.Copy(env, bindingEnv)
		allowedHosts = appendAllowedHost(allowedHosts, relayHost)
	}
	if deps.Egress.DefaultAction == egress.PolicyDeny {
		proxyEnv, err := buildHostedRuntimePublicEgressProxy(name, sessionID, entry.AllowedHosts, deps.Egress.DefaultAction, deps)
		if err != nil {
			return providerdev.RuntimeEnv{}, err
		}
		maps.Copy(env, proxyEnv)
		if _, proxyHost, err := pluginRuntimePublicProxyBaseURL(deps.BaseURL); err == nil {
			allowedHosts = appendAllowedHost(allowedHosts, proxyHost)
		}
	}

	cleanupOnError = false
	return providerdev.RuntimeEnv{
		Env:          env,
		AllowedHosts: slices.Clone(allowedHosts),
		Cleanup:      cleanup,
	}, nil
}
