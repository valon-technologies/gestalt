package bootstrap

import (
	"context"
	"fmt"
	"sync"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/appruntime"
)

type pluginRuntimeRegistry struct {
	cfg     *config.Config
	deps    Deps
	factory RuntimeFactory

	mu        sync.Mutex
	providers map[string]appruntime.Provider
	closed    bool
}

func newAppRuntimeRegistry(cfg *config.Config, factory RuntimeFactory, deps Deps) *pluginRuntimeRegistry {
	if cfg == nil {
		return nil
	}
	return &pluginRuntimeRegistry{
		cfg:       cfg,
		deps:      deps,
		factory:   factory,
		providers: make(map[string]appruntime.Provider, len(cfg.Runtime.Providers)),
	}
}

func (r *pluginRuntimeRegistry) Resolve(ctx context.Context, configPath string, entry *config.ProviderEntry) (config.EffectiveRuntimePlacement, appruntime.Provider, error) {
	if r == nil || r.cfg == nil {
		return config.EffectiveRuntimePlacement{}, nil, nil
	}

	effective, err := r.cfg.EffectiveRuntimePlacement(configPath, entry)
	if err != nil {
		return config.EffectiveRuntimePlacement{}, nil, err
	}
	if !effective.Enabled || effective.ProviderName == "" || effective.Provider == nil {
		return effective, nil, nil
	}

	provider, err := r.resolveConfigured(ctx, effective)
	return effective, provider, err
}

func (r *pluginRuntimeRegistry) resolveConfigured(ctx context.Context, effective config.EffectiveRuntimePlacement) (appruntime.Provider, error) {
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
	var provider appruntime.Provider
	switch effective.Provider.Driver {
	case config.RuntimeProviderDriverLocal:
		provider = newLocalAppRuntime(providerName, r.deps)
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
	providers := make([]appruntime.Provider, 0, len(r.providers))
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

func closeAppRuntimeRegistry(registry *pluginRuntimeRegistry) error {
	if registry == nil {
		return nil
	}
	return registry.Close()
}
