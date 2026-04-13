package registry

import (
	"fmt"
	"slices"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
)

// ProviderMap is a thread-safe, named collection of providers of a single type.
type ProviderMap[T any] struct {
	mu    sync.RWMutex
	items map[string]T
	kind  string
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
	return nil
}

func (m *ProviderMap[T]) Get(name string) (T, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.items[name]
	if !ok {
		var zero T
		return zero, fmt.Errorf("%s %q: %w", m.kind, name, core.ErrNotFound)
	}
	return val, nil
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
	AuthProviders ProviderMap[core.AuthProvider]
	Providers     ProviderMap[core.Provider]
}

func New() *Registry {
	return &Registry{
		AuthProviders: newProviderMap[core.AuthProvider]("auth provider"),
		Providers:     newProviderMap[core.Provider]("provider"),
	}
}
