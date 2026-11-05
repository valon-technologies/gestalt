package bootstrap

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// AppDefinitions is the runtime view of registry-installed apps.
//
// Static routing (connection maps, connection runtime, the server's app
// definitions) is built from config at boot. A registry-only app's config
// entry carries no manifest then, because its package is installed later by
// the registry poller, so the static view has none of the manifest's
// connections or display metadata. When a registry app version starts, the
// restarter publishes the mounted entry and the routing derived from it here;
// when it stops, the restarter clears it. Readers consult this overlay first
// and fall back to the static view, so path- and lock-sourced apps, which
// never appear here, are unaffected.
//
// All methods are safe for concurrent use and on a nil receiver.
type AppDefinitions struct {
	mu         sync.RWMutex
	apps       map[string]*appDefinition
	generation atomic.Uint64
}

type appDefinition struct {
	entry       *config.ProviderEntry
	connections map[invocation.ConnectionSurface]string
	runtime     map[string]invocation.ConnectionRuntimeInfo
}

func NewAppDefinitions() *AppDefinitions {
	return &AppDefinitions{apps: map[string]*appDefinition{}}
}

// AppEntry returns the published entry for name, if a registry-installed
// version of it is running.
func (d *AppDefinitions) AppEntry(name string) (*config.ProviderEntry, bool) {
	def := d.lookup(name)
	if def == nil {
		return nil, false
	}
	return def.entry, true
}

// ConnectionName implements invocation.ConnectionOverrides.
func (d *AppDefinitions) ConnectionName(surface invocation.ConnectionSurface, provider string) (string, bool) {
	def := d.lookup(provider)
	if def == nil {
		return "", false
	}
	return def.connections[surface], true
}

// ConnectionRuntime returns a resolver that answers from the published
// runtime for registry-installed apps and from static otherwise.
func (d *AppDefinitions) ConnectionRuntime(static invocation.ConnectionRuntimeResolver) invocation.ConnectionRuntimeResolver {
	return func(provider, connection string) (invocation.ConnectionRuntimeInfo, bool) {
		if def := d.lookup(provider); def != nil {
			connection = strings.TrimSpace(connection)
			if connection == "" {
				connection = config.AppConnectionName
			}
			info, ok := def.runtime[connection]
			return info, ok
		}
		if static == nil {
			return invocation.ConnectionRuntimeInfo{}, false
		}
		return static(provider, connection)
	}
}

// Generation changes whenever a definition is published or cleared, so
// caches keyed on app definitions can invalidate.
func (d *AppDefinitions) Generation() uint64 {
	if d == nil {
		return 0
	}
	return d.generation.Load()
}

func (d *AppDefinitions) lookup(name string) *appDefinition {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.apps[strings.TrimSpace(name)]
}

// publish derives connection routing and runtime from a mounted registry
// entry and makes them visible. The entry must not be mutated afterwards.
func (d *AppDefinitions) publish(cfg *config.Config, name string, entry *config.ProviderEntry, deps Deps) error {
	if d == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" || entry == nil {
		return fmt.Errorf("publish app definition: app name and entry are required")
	}
	plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
	if err != nil {
		return fmt.Errorf("publish app definition %q: %w", name, err)
	}
	runtime, err := providerConnectionRuntime("integration", name, entry, newEgressDeps(cfg))
	if err != nil {
		return fmt.Errorf("publish app definition: %w", err)
	}
	if err := attachProviderMCPOAuthRuntimeAuth(name, entry, runtime, deps); err != nil {
		return fmt.Errorf("publish app definition: %w", err)
	}
	def := &appDefinition{
		entry: entry,
		connections: map[invocation.ConnectionSurface]string{
			invocation.ConnectionSurfaceDefault: plan.AuthDefaultConnection(),
			invocation.ConnectionSurfaceAPI:     plan.APIConnection(),
			invocation.ConnectionSurfaceMCP:     plan.MCPConnection(),
		},
		runtime: runtime,
	}
	d.mu.Lock()
	d.apps[name] = def
	d.mu.Unlock()
	d.generation.Add(1)
	return nil
}

func (d *AppDefinitions) clear(name string) {
	if d == nil {
		return
	}
	name = strings.TrimSpace(name)
	d.mu.Lock()
	_, existed := d.apps[name]
	delete(d.apps, name)
	d.mu.Unlock()
	if existed {
		d.generation.Add(1)
	}
}
