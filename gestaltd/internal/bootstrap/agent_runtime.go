package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type agentRuntime struct {
	mu                     sync.RWMutex
	defaultProviderName    string
	configuredProviders    map[string]struct{}
	startupFailedProviders map[string]struct{}
	providers              map[string]coreagent.Provider
	pendingProviders       map[string]*startupProviderHandle[coreagent.Provider]
	startupWaits           *startupWaitTracker
}

func newAgentRuntime(cfg *config.Config, startupWaits *startupWaitTracker) (*agentRuntime, error) {
	if startupWaits == nil {
		startupWaits = newStartupWaitTracker()
	}
	runtime := &agentRuntime{
		configuredProviders:    map[string]struct{}{},
		startupFailedProviders: map[string]struct{}{},
		providers:              map[string]coreagent.Provider{},
		pendingProviders:       map[string]*startupProviderHandle[coreagent.Provider]{},
		startupWaits:           startupWaits,
	}
	if cfg != nil {
		selectedProviderName, _, err := cfg.SelectedAgentProvider()
		if err == nil {
			runtime.defaultProviderName = strings.TrimSpace(selectedProviderName)
		}
		for name, entry := range cfg.Providers.Agent {
			name = strings.TrimSpace(name)
			if name == "" || entry == nil {
				continue
			}
			runtime.configuredProviders[name] = struct{}{}
			runtime.pendingProviders[name] = newAgentProviderHandle(name, startupWaits)
		}
	}
	return runtime, nil
}

func newAgentProviderHandle(name string, tracker *startupWaitTracker) *startupProviderHandle[coreagent.Provider] {
	return newStartupProviderHandle[coreagent.Provider](name, newStartupProviderNode(invocation.ProviderKindAgent, name), tracker)
}

func agentSessionStartConfigs(cfg *config.Config) map[string]*coreagent.SessionStartConfig {
	if cfg == nil || len(cfg.Providers.Agent) == 0 {
		return nil
	}
	out := make(map[string]*coreagent.SessionStartConfig)
	for name, entry := range cfg.Providers.Agent {
		name = strings.TrimSpace(name)
		if name == "" || entry == nil || entry.Lifecycle == nil || len(entry.Lifecycle.SessionStart) == 0 {
			continue
		}
		hooks := make([]coreagent.SessionStartHook, 0, len(entry.Lifecycle.SessionStart))
		for _, hook := range entry.Lifecycle.SessionStart {
			hooks = append(hooks, coreagent.SessionStartHook{
				ID:      hook.ID,
				Type:    hook.Type,
				Command: append([]string(nil), hook.Command...),
				CWD:     hook.CWD,
				Timeout: hook.Timeout,
				Env:     maps.Clone(hook.Env),
				Output: coreagent.SessionStartHookOutput{
					AdditionalContext: hook.Output.AdditionalContext,
					Metadata:          hook.Output.Metadata,
				},
			})
		}
		if len(hooks) > 0 {
			out[name] = &coreagent.SessionStartConfig{Hooks: hooks}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *agentRuntime) PublishProvider(name string, provider coreagent.Provider) {
	name = strings.TrimSpace(name)
	if r == nil || provider == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = map[string]coreagent.Provider{}
	}
	if r.pendingProviders == nil {
		r.pendingProviders = map[string]*startupProviderHandle[coreagent.Provider]{}
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

func (r *agentRuntime) FailStartupProvider(name string, err error) {
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
		err = agentmanager.NewAgentProviderNotAvailableError(name)
	}
	handle.fail(err)
	if r.startupFailedProviders == nil {
		r.startupFailedProviders = map[string]struct{}{}
	}
	r.startupFailedProviders[name] = struct{}{}
	delete(r.pendingProviders, name)
}

func (r *agentRuntime) UnpublishProvider(name string) {
	name = strings.TrimSpace(name)
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, name)
	delete(r.pendingProviders, name)
}

func (r *agentRuntime) FailPendingProviders(err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, handle := range r.pendingProviders {
		failErr := err
		if failErr == nil {
			failErr = agentmanager.NewAgentProviderNotAvailableError(name)
		}
		handle.fail(failErr)
		if r.startupFailedProviders == nil {
			r.startupFailedProviders = map[string]struct{}{}
		}
		r.startupFailedProviders[name] = struct{}{}
		delete(r.pendingProviders, name)
	}
}

func (r *agentRuntime) HasConfiguredProviders() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.configuredProviders) > 0 || len(r.providers) > 0 || len(r.pendingProviders) > 0
}

func (r *agentRuntime) ResolveProvider(ctx context.Context, name string) (string, coreagent.Provider, error) {
	if r == nil {
		return "", nil, fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	selectedName := strings.TrimSpace(name)
	if selectedName == "" {
		selectedName = strings.TrimSpace(r.defaultProviderName)
	}
	if selectedName == "" {
		r.mu.RUnlock()
		return "", nil, agentmanager.ErrAgentProviderRequired
	}
	provider, ok := r.providers[selectedName]
	handle := r.pendingProviders[selectedName]
	r.mu.RUnlock()
	if ok && provider != nil {
		return selectedName, provider, nil
	}
	if handle == nil {
		return "", nil, agentmanager.NewAgentProviderNotAvailableError(selectedName)
	}
	provider, err := handle.await(ctx)
	if err != nil {
		return "", nil, err
	}
	if provider == nil {
		return "", nil, agentmanager.NewAgentProviderNotAvailableError(selectedName)
	}
	return selectedName, provider, nil
}

func (r *agentRuntime) ProviderNames() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]struct{}{}
	names := make([]string, 0, len(r.providers)+len(r.pendingProviders))
	for name := range r.providers {
		if strings.TrimSpace(name) == "" {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for name := range r.pendingProviders {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *agentRuntime) Ping(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	defaultProviderName := strings.TrimSpace(r.defaultProviderName)
	providers := maps.Clone(r.providers)
	pendingProviders := maps.Clone(r.pendingProviders)
	configuredProviders := make(map[string]struct{}, len(r.configuredProviders))
	for name := range r.configuredProviders {
		name = strings.TrimSpace(name)
		if name != "" {
			configuredProviders[name] = struct{}{}
		}
	}
	r.mu.RUnlock()

	if len(configuredProviders) == 0 {
		for name, provider := range providers {
			name = strings.TrimSpace(name)
			if name != "" && provider != nil {
				configuredProviders[name] = struct{}{}
			}
		}
	}
	if defaultProviderName != "" {
		configuredProviders[defaultProviderName] = struct{}{}
	}
	if len(configuredProviders) == 0 {
		return nil
	}

	names := make([]string, 0, len(configuredProviders))
	for name := range configuredProviders {
		names = append(names, name)
	}
	sort.Strings(names)
	errs := make(chan error, len(names))
	var wg sync.WaitGroup
	for _, name := range names {
		provider := providers[name]
		if provider == nil {
			if handle := pendingProviders[name]; handle != nil {
				if resolved, ready, err := handle.resolved(); err != nil {
					errs <- fmt.Errorf("agent provider %q unavailable: %w", name, err)
					continue
				} else if ready {
					provider = resolved
				}
			}
			if provider == nil {
				errs <- fmt.Errorf("agent provider %q unavailable: %w", name, agentmanager.NewAgentProviderNotAvailableError(name))
				continue
			}
		}
		wg.Add(1)
		go func(name string, provider coreagent.Provider) {
			defer wg.Done()
			if err := provider.Ping(ctx); err != nil {
				errs <- fmt.Errorf("agent provider %q unavailable: %w", name, err)
			}
		}(name, provider)
	}
	wg.Wait()
	close(errs)
	var joined []error
	for err := range errs {
		joined = append(joined, err)
	}
	return errors.Join(joined...)
}
