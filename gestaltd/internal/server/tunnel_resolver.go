package server

import (
	"context"
	"crypto/tls"
	"time"
	"fmt"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	coredata "github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/remotepublish"
)

// TunnelResolverConfig holds the upstream-side tunnel dial parameters needed
// to build tunnel proxy providers for remote-registered apps.
type TunnelResolverConfig struct {
	RemoteRegistrations *coredata.RemoteRegistrationService
	ConnectAddr         string
	ClientIdentity      tls.Certificate
}

// tunnelProviderResolver implements registry.RemoteResolver[core.Provider]. It
// consults the RemoteRegistrationService for tunnel-registered apps and builds
// (and caches) a TunnelProxyProvider for each. Tunnel registrations always
// take precedence over local providers (tunnel always wins).
type tunnelProviderResolver struct {
	cfg          TunnelResolverConfig
	mu           sync.Mutex
	cache        map[string]*remotepublish.TunnelProxyProvider
	cacheGen     map[string]uint64
}

func newTunnelProviderResolver(cfg TunnelResolverConfig) *tunnelProviderResolver {
	if cfg.RemoteRegistrations == nil || strings.TrimSpace(cfg.ConnectAddr) == "" {
		return nil
	}
	return &tunnelProviderResolver{
		cfg:      cfg,
		cache:    make(map[string]*remotepublish.TunnelProxyProvider),
		cacheGen: make(map[string]uint64),
	}
}

func (r *tunnelProviderResolver) ResolveProvider(ctx context.Context, name string) (core.Provider, error) {
	if r == nil || r.cfg.RemoteRegistrations == nil {
		return nil, core.ErrNotFound
	}

	remoteProvider, reg, err := r.cfg.RemoteRegistrations.ResolveProvider(ctx, "app", name)
	if err != nil {
		return nil, core.ErrNotFound
	}
	if remoteProvider == nil || reg == nil {
		return nil, core.ErrNotFound
	}

	r.mu.Lock()
	cached := r.cache[name]
	cachedGen := r.cacheGen[name]
	r.mu.Unlock()

	// Return cached provider if the registration generation hasn't changed.
	if cached != nil && cachedGen == reg.Generation {
		return cached, nil
	}

	// Build a new tunnel proxy provider. Use a fresh context with a timeout
	// for the metadata fetch.
	buildCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	provider, err := remotepublish.NewTunnelProxyProvider(buildCtx, remotepublish.TunnelProxyConfig{
		AppName:        name,
		TunnelHost:     reg.TunnelHost,
		PinnedSPKI:     reg.ServerSPKISHA256,
		ConnectAddr:    r.cfg.ConnectAddr,
		ClientIdentity: r.cfg.ClientIdentity,
	})
	if err != nil {
		return nil, fmt.Errorf("tunnel resolver: build proxy for %s: %w", name, err)
	}

	// Close the old cached provider if it exists.
	r.mu.Lock()
	if old, ok := r.cache[name]; ok && old != cached {
		_ = old.Close()
	}
	r.cache[name] = provider
	r.cacheGen[name] = reg.Generation
	r.mu.Unlock()

	return provider, nil
}

// Close shuts down all cached tunnel proxy providers.
func (r *tunnelProviderResolver) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.cache {
		_ = p.Close()
	}
	r.cache = nil
}

var _ registry.RemoteResolver[core.Provider] = (*tunnelProviderResolver)(nil)
