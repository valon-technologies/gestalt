package bootstrap

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

type workflowRuntime struct {
	mu                  sync.RWMutex
	defaultProviderName string
	configuredProviders map[string]struct{}
	providers           map[string]coreworkflow.Provider
	startupWaits        *startupWaitTracker
}

func newWorkflowRuntime(cfg *config.Config) (*workflowRuntime, error) {
	runtime := &workflowRuntime{
		configuredProviders: map[string]struct{}{},
		providers:           map[string]coreworkflow.Provider{},
		startupWaits:        newStartupWaitTracker(),
	}
	if cfg != nil {
		selectedProviderName, _, err := cfg.SelectedWorkflowProvider()
		if err == nil {
			runtime.defaultProviderName = strings.TrimSpace(selectedProviderName)
		}
	}
	return runtime, nil
}

func (r *workflowRuntime) InitProviderPlaceholders(defs map[string]*config.ProviderEntry) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = map[string]coreworkflow.Provider{}
	}
	for name, entry := range defs {
		name = strings.TrimSpace(name)
		if name == "" || entry == nil {
			continue
		}
		if r.configuredProviders == nil {
			r.configuredProviders = map[string]struct{}{}
		}
		r.configuredProviders[name] = struct{}{}
		if _, exists := r.providers[name]; exists {
			continue
		}
		r.providers[name] = newStartupWorkflowProviderProxy(name, r.startupWaits)
	}
}

func (r *workflowRuntime) PublishProvider(name string, provider coreworkflow.Provider) {
	name = strings.TrimSpace(name)
	if r == nil || name == "" || provider == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, exists := r.providers[name]
	if _, configured := r.configuredProviders[name]; configured && !exists {
		return
	}
	if proxy, ok := existing.(*startupWorkflowProviderProxy); ok {
		proxy.publish(provider)
	}
	r.providers[name] = provider
}

func (r *workflowRuntime) FailProvider(name string, err error) {
	name = strings.TrimSpace(name)
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if proxy, ok := r.providers[name].(*startupWorkflowProviderProxy); ok {
		proxy.fail(err)
	}
	delete(r.providers, name)
}

func (r *workflowRuntime) FailPendingProviders(err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, provider := range r.providers {
		proxy, ok := provider.(*startupWorkflowProviderProxy)
		if !ok {
			continue
		}
		proxy.fail(err)
		delete(r.providers, name)
	}
}

func (r *workflowRuntime) StartupWaitTracker() *startupWaitTracker {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.startupWaits
}

func (r *workflowRuntime) HasConfiguredProviders() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.providers) > 0
}

func (r *workflowRuntime) ResolveProvider(name string) (coreworkflow.Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[strings.TrimSpace(name)]
	if !ok || provider == nil {
		return nil, fmt.Errorf("workflow provider %q is not available", name)
	}
	return provider, nil
}

func (r *workflowRuntime) ResolveProviderSelection(name string) (string, coreworkflow.Provider, error) {
	if r == nil {
		return "", nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	selectedName := strings.TrimSpace(name)
	if selectedName == "" {
		selectedName = strings.TrimSpace(r.defaultProviderName)
	}
	if selectedName == "" {
		return "", nil, fmt.Errorf("workflow provider is required")
	}
	provider, ok := r.providers[selectedName]
	if !ok || provider == nil {
		return "", nil, fmt.Errorf("workflow provider %q is not available", selectedName)
	}
	return selectedName, provider, nil
}

func (r *workflowRuntime) ProviderNames() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
