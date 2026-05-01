package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
)

type pluginRuntimeRegistry struct {
	cfg     *config.Config
	deps    Deps
	factory RuntimeFactory

	mu        sync.Mutex
	providers map[string]pluginruntime.Provider
	closed    bool
}

func newPluginRuntimeRegistry(cfg *config.Config, factory RuntimeFactory, deps Deps) *pluginRuntimeRegistry {
	if cfg == nil {
		return nil
	}
	return &pluginRuntimeRegistry{
		cfg:       cfg,
		deps:      deps,
		factory:   factory,
		providers: make(map[string]pluginruntime.Provider, len(cfg.Runtime.Providers)),
	}
}

func (r *pluginRuntimeRegistry) Resolve(ctx context.Context, configPath string, entry *config.ProviderEntry) (config.EffectiveHostedRuntime, pluginruntime.Provider, error) {
	if r == nil || r.cfg == nil {
		return config.EffectiveHostedRuntime{}, nil, nil
	}

	effective, err := r.cfg.EffectiveHostedRuntime(configPath, entry)
	if err != nil {
		return config.EffectiveHostedRuntime{}, nil, err
	}
	if !effective.Enabled || effective.ProviderName == "" || effective.Provider == nil {
		return effective, nil, nil
	}

	provider, err := r.resolveConfigured(ctx, effective)
	return effective, provider, err
}

func (r *pluginRuntimeRegistry) VerifyPluginRuntimeSession(ctx context.Context, providerName, sessionID string) error {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return fmt.Errorf("runtime provider owner is required")
	}
	if r == nil || r.cfg == nil {
		return fmt.Errorf("%w: plugin runtime registry is not configured", runtimehost.ErrProviderNotHostedRuntime)
	}

	effective, err := r.effectiveHostedRuntimeForProvider(providerName)
	if err != nil {
		return err
	}
	if !effective.Enabled || strings.TrimSpace(effective.ProviderName) == "" {
		return fmt.Errorf("%w: provider %q", runtimehost.ErrProviderNotHostedRuntime, providerName)
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("plugin runtime registry is closed")
	}
	runtimeProvider := r.providers[effective.ProviderName]
	r.mu.Unlock()
	if runtimeProvider == nil {
		return fmt.Errorf("runtime provider %q is not loaded", effective.ProviderName)
	}

	return runtimeHostServiceSessionVerifier{
		providerName: providerName,
		provider:     runtimeProvider,
	}.VerifyHostServiceSession(ctx, sessionID)
}

func (r *pluginRuntimeRegistry) effectiveHostedRuntimeForProvider(providerName string) (config.EffectiveHostedRuntime, error) {
	if r == nil || r.cfg == nil {
		return config.EffectiveHostedRuntime{}, fmt.Errorf("plugin runtime registry is not configured")
	}
	if entry := r.cfg.Plugins[providerName]; entry != nil {
		return r.cfg.EffectiveHostedRuntime("plugins."+providerName, entry)
	}
	if entry := r.cfg.Providers.Agent[providerName]; entry != nil {
		return r.cfg.EffectiveHostedRuntime("providers.agent."+providerName, entry)
	}
	return config.EffectiveHostedRuntime{}, fmt.Errorf("%w: provider %q does not have a hosted runtime configuration", runtimehost.ErrProviderNotHostedRuntime, providerName)
}

func (r *pluginRuntimeRegistry) resolveConfigured(ctx context.Context, effective config.EffectiveHostedRuntime) (pluginruntime.Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("plugin runtime registry is not configured")
	}
	providerName := effective.ProviderName
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("plugin runtime registry is closed")
	}
	if provider, ok := r.providers[providerName]; ok && provider != nil {
		r.mu.Unlock()
		return provider, nil
	}
	factory := r.factory
	r.mu.Unlock()
	var provider pluginruntime.Provider
	switch effective.Provider.Driver {
	case config.RuntimeProviderDriverLocal:
		provider = newLocalPluginRuntime(providerName, r.deps)
	default:
		if factory == nil {
			return nil, fmt.Errorf("runtime provider %q is not registered", providerName)
		}
		var err error
		provider, err = factory(ctx, providerName, effective.Provider, r.deps)
		if err != nil {
			return nil, fmt.Errorf("build runtime provider %q: %w", providerName, err)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		_ = provider.Close()
		return nil, fmt.Errorf("plugin runtime registry is closed")
	}
	if existing, ok := r.providers[providerName]; ok && existing != nil {
		_ = provider.Close()
		return existing, nil
	}
	r.providers[providerName] = provider
	return provider, nil
}

func (r *pluginRuntimeRegistry) Close() error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	providers := make([]pluginruntime.Provider, 0, len(r.providers))
	for _, provider := range r.providers {
		if provider != nil {
			providers = append(providers, provider)
		}
	}
	r.providers = nil
	r.mu.Unlock()

	var firstErr error
	for _, provider := range providers {
		if err := provider.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func closePluginRuntimeRegistry(registry *pluginRuntimeRegistry) error {
	if registry == nil {
		return nil
	}
	return registry.Close()
}
