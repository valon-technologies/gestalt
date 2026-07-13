package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type workflowRuntime struct {
	mu                     sync.RWMutex
	defaultProviderName    string
	configuredProviders    map[string]struct{}
	startupFailedProviders map[string]struct{}
	providers              map[string]coreworkflow.Provider
	pendingProviders       map[string]*startupProviderHandle[coreworkflow.Provider]
	startupWaits           *startupWaitTracker
}

func newWorkflowRuntime(cfg *config.Config) (*workflowRuntime, error) {
	runtime := &workflowRuntime{
		configuredProviders:    map[string]struct{}{},
		startupFailedProviders: map[string]struct{}{},
		providers:              map[string]coreworkflow.Provider{},
		pendingProviders:       map[string]*startupProviderHandle[coreworkflow.Provider]{},
		startupWaits:           newStartupWaitTracker(),
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
	if r.pendingProviders == nil {
		r.pendingProviders = map[string]*startupProviderHandle[coreworkflow.Provider]{}
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
		if _, exists := r.pendingProviders[name]; exists {
			continue
		}
		r.pendingProviders[name] = newWorkflowProviderHandle(name, r.startupWaits)
	}
}

func newWorkflowProviderHandle(name string, tracker *startupWaitTracker) *startupProviderHandle[coreworkflow.Provider] {
	return newStartupProviderHandle[coreworkflow.Provider](name, newStartupProviderNode(invocation.ProviderKindWorkflow, name), tracker)
}

func (r *workflowRuntime) PublishProvider(name string, provider coreworkflow.Provider) {
	name = strings.TrimSpace(name)
	if r == nil || name == "" || provider == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = map[string]coreworkflow.Provider{}
	}
	if r.pendingProviders == nil {
		r.pendingProviders = map[string]*startupProviderHandle[coreworkflow.Provider]{}
	}
	if r.startupFailedProviders == nil {
		r.startupFailedProviders = map[string]struct{}{}
	}
	if _, failedStartup := r.startupFailedProviders[name]; failedStartup {
		return
	}
	if handle := r.pendingProviders[name]; handle != nil {
		handle.publish(provider)
		delete(r.pendingProviders, name)
	}
	r.providers[name] = provider
}

func (r *workflowRuntime) FailStartupProvider(name string, err error) {
	name = strings.TrimSpace(name)
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	handle := r.pendingProviders[name]
	if handle == nil {
		return
	}
	if err == nil {
		err = fmt.Errorf("workflow provider %q is not available", name)
	}
	handle.fail(err)
	if r.startupFailedProviders == nil {
		r.startupFailedProviders = map[string]struct{}{}
	}
	r.startupFailedProviders[name] = struct{}{}
	delete(r.pendingProviders, name)
}

func (r *workflowRuntime) UnpublishProvider(name string) {
	name = strings.TrimSpace(name)
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, name)
	delete(r.pendingProviders, name)
}

func (r *workflowRuntime) FailPendingProviders(err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, handle := range r.pendingProviders {
		failErr := err
		if failErr == nil {
			failErr = fmt.Errorf("workflow provider %q is not available", name)
		}
		handle.fail(failErr)
		if r.startupFailedProviders == nil {
			r.startupFailedProviders = map[string]struct{}{}
		}
		r.startupFailedProviders[name] = struct{}{}
		delete(r.pendingProviders, name)
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
	return len(r.providers) > 0 || len(r.pendingProviders) > 0
}

func (r *workflowRuntime) ResolveProvider(ctx context.Context, name string) (string, coreworkflow.Provider, error) {
	if r == nil {
		return "", nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	selectedName := strings.TrimSpace(name)
	if selectedName == "" {
		selectedName = strings.TrimSpace(r.defaultProviderName)
	}
	if selectedName == "" {
		r.mu.RUnlock()
		return "", nil, fmt.Errorf("workflow provider is required")
	}
	provider, ok := r.providers[selectedName]
	handle := r.pendingProviders[selectedName]
	r.mu.RUnlock()
	if ok && provider != nil {
		return selectedName, provider, nil
	}
	if handle == nil {
		return "", nil, fmt.Errorf("workflow provider %q is not available", selectedName)
	}
	provider, err := handle.await(ctx)
	if err != nil {
		return "", nil, err
	}
	if provider == nil {
		return "", nil, fmt.Errorf("workflow provider %q is not available", selectedName)
	}
	return selectedName, provider, nil
}

func (r *workflowRuntime) DefaultProviderName() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return strings.TrimSpace(r.defaultProviderName)
}

func (r *workflowRuntime) ProviderNames() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	name := strings.TrimSpace(r.defaultProviderName)
	if name == "" {
		return nil
	}
	return []string{name}
}
