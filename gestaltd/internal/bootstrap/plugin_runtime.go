package bootstrap

import (
	"context"
	"fmt"
	"sync"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
)

type pluginRuntimeFactory func(ctx context.Context, name string, entry *config.RuntimeProviderEntry, deps Deps) (pluginruntime.Provider, error)

type pluginRuntimeRegistry struct {
	cfg       *config.Config
	deps      Deps
	factories map[config.RuntimeProviderDriver]pluginRuntimeFactory

	mu        sync.Mutex
	providers map[string]pluginruntime.Provider
	closed    bool
}

func newPluginRuntimeRegistry(cfg *config.Config, factories map[config.RuntimeProviderDriver]pluginRuntimeFactory, deps Deps) *pluginRuntimeRegistry {
	if cfg == nil {
		return nil
	}
	return &pluginRuntimeRegistry{
		cfg:       cfg,
		deps:      deps,
		factories: factories,
		providers: make(map[string]pluginruntime.Provider, len(cfg.Runtime.Providers)),
	}
}

func (r *pluginRuntimeRegistry) Resolve(ctx context.Context, pluginName string, entry *config.ProviderEntry) (config.EffectivePluginRuntime, pluginruntime.Provider, error) {
	if r == nil || r.cfg == nil {
		return config.EffectivePluginRuntime{}, nil, nil
	}

	effective, err := r.cfg.EffectivePluginRuntime(pluginName, entry)
	if err != nil {
		return config.EffectivePluginRuntime{}, nil, err
	}
	if !effective.Enabled || effective.ProviderName == "" || effective.Provider == nil {
		return effective, nil, nil
	}

	providerName := effective.ProviderName
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return config.EffectivePluginRuntime{}, nil, fmt.Errorf("plugin runtime registry is closed")
	}
	if provider, ok := r.providers[providerName]; ok && provider != nil {
		r.mu.Unlock()
		return effective, provider, nil
	}
	factory := r.factories[effective.Provider.Driver]
	r.mu.Unlock()
	if factory == nil {
		return config.EffectivePluginRuntime{}, nil, fmt.Errorf("runtime provider %q driver %q is not registered", providerName, effective.Provider.Driver)
	}

	provider, err := factory(ctx, providerName, effective.Provider, r.deps)
	if err != nil {
		return config.EffectivePluginRuntime{}, nil, fmt.Errorf("build runtime provider %q: %w", providerName, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		_ = provider.Close()
		return config.EffectivePluginRuntime{}, nil, fmt.Errorf("plugin runtime registry is closed")
	}
	if existing, ok := r.providers[providerName]; ok && existing != nil {
		_ = provider.Close()
		return effective, existing, nil
	}
	r.providers[providerName] = provider
	return effective, provider, nil
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
