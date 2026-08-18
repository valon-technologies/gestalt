package registry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/valon-technologies/gestalt/server/core"
)

// RemoteResolver resolves a provider name to a remote (tunnel-registered)
// provider. When set on a ProviderMap, Get consults the resolver before the
// local map; if the resolver returns a provider, it takes precedence (tunnel
// always wins). This implements reverse-tunnel traffic forwarding: a
// tunnel-registered app shadows any local provider with the same name.
type RemoteResolver[T any] interface {
	ResolveProvider(ctx context.Context, name string) (T, error)
}

// ProviderMap is a thread-safe, named collection of providers of a single type.
type ProviderMap[T any] struct {
	mu         sync.RWMutex
	items      map[string]T
	kind       string
	resolver   RemoteResolver[T]
	generation atomic.Uint64
}

func newProviderMap[T any](kind string) ProviderMap[T] {
	return ProviderMap[T]{items: make(map[string]T), kind: kind}
}

func (m *ProviderMap[T]) Register(name string, val T) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.items[name]; exists {
		return fmt.Errorf("%s %q: %w", m.kind, name, core.ErrAlreadyRegistered)
	}
	m.items[name] = val
	m.generation.Add(1)
	return nil
}

func (m *ProviderMap[T]) Replace(name string, val T) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.items[name]; !exists {
		return fmt.Errorf("%s %q: %w", m.kind, name, core.ErrNotFound)
	}
	m.items[name] = val
	m.generation.Add(1)
	return nil
}

func (m *ProviderMap[T]) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, name)
	m.generation.Add(1)
}

// Generation bumps whenever the local map or remote resolver changes so
// callers can drop a cached directory instead of serving replaced apps.
func (m *ProviderMap[T]) Generation() uint64 {
	if m == nil {
		return 0
	}
	return m.generation.Load()
}

func (m *ProviderMap[T]) Get(name string) (T, error) {
	return m.GetWithContext(context.Background(), name)
}

// GetWithContext resolves a provider by name. If a RemoteResolver is set, it is
// consulted first (tunnel always wins). If the resolver returns a provider, it
// shadows any local provider with the same name. If the resolver returns
// core.ErrNotFound (no tunnel registration exists), the local map is consulted
// as a fallback. Any other resolver error (registration exists but proxy
// construction failed) is propagated to the caller so the request fails rather
// than silently serving a local provider.
func (m *ProviderMap[T]) GetWithContext(ctx context.Context, name string) (T, error) {
	if m != nil && m.resolver != nil {
		val, err := m.resolver.ResolveProvider(ctx, name)
		if err == nil {
			return val, nil
		}
		if !errors.Is(err, core.ErrNotFound) {
			var zero T
			return zero, err
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.items[name]
	if !ok {
		var zero T
		return zero, fmt.Errorf("%s %q: %w", m.kind, name, core.ErrNotFound)
	}
	return val, nil
}

// SetRemoteResolver sets the remote resolver consulted by GetWithContext. Once
// set, tunnel-registered providers take precedence over local providers for the
// same name. Pass nil to disable.
func (m *ProviderMap[T]) SetRemoteResolver(r RemoteResolver[T]) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.resolver = r
	m.mu.Unlock()
	m.generation.Add(1)
}

// List returns all registered names, sorted alphabetically.
func (m *ProviderMap[T]) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.items))
	for name := range m.items {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

type Registry struct {
	AuthProviders ProviderMap[core.IdentityProvider]
	Providers     ProviderMap[core.Provider]
}

func New() *Registry {
	return &Registry{
		AuthProviders: newProviderMap[core.IdentityProvider]("identity provider"),
		Providers:     newProviderMap[core.Provider]("provider"),
	}
}
