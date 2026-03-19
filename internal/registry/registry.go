package registry

import (
	"fmt"
	"slices"
	"sync"

	"github.com/valon-technologies/toolshed/core"
)

// PluginMap is a thread-safe, named collection of plugins of a single type.
type PluginMap[T any] struct {
	mu    sync.RWMutex
	items map[string]T
	kind  string
}

func newPluginMap[T any](kind string) PluginMap[T] {
	return PluginMap[T]{items: make(map[string]T), kind: kind}
}

func (m *PluginMap[T]) Register(name string, val T) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.items[name]; exists {
		return fmt.Errorf("%s %q: %w", m.kind, name, core.ErrAlreadyRegistered)
	}
	m.items[name] = val
	return nil
}

func (m *PluginMap[T]) Get(name string) (T, error) {
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
func (m *PluginMap[T]) List() []string {
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
	Datastores    PluginMap[core.Datastore]
	AuthProviders PluginMap[core.AuthProvider]
	Providers     PluginMap[core.Provider]
	Runtimes      PluginMap[core.Runtime]
}

func New() *Registry {
	return &Registry{
		Datastores:    newPluginMap[core.Datastore]("datastore"),
		AuthProviders: newPluginMap[core.AuthProvider]("auth provider"),
		Providers:     newPluginMap[core.Provider]("provider"),
		Runtimes:      newPluginMap[core.Runtime]("runtime"),
	}
}

func NewRuntimeMap() *PluginMap[core.Runtime] {
	m := newPluginMap[core.Runtime]("runtime")
	return &m
}

func NewBindingMap() *PluginMap[core.Binding] {
	m := newPluginMap[core.Binding]("binding")
	return &m
}
