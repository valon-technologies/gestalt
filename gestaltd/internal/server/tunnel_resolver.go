package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coredata "github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/remotepublish"
	"golang.org/x/sync/singleflight"
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
//
// When a registration exists but proxy construction fails (e.g. the tunnel is
// unreachable), ResolveProvider returns a non-ErrNotFound error so the caller
// fails the request rather than silently falling back to a local provider.
type tunnelProviderResolver struct {
	cfg      TunnelResolverConfig
	mu       sync.Mutex
	cache    map[string]*remotepublish.TunnelProxyProvider
	cacheGen map[string]uint64
	group    singleflight.Group
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

// ErrTunnelProviderUnavailable is returned when a tunnel registration exists
// for an app but the proxy provider could not be built (e.g. tunnel is down).
// It is distinct from core.ErrNotFound so ProviderMap.GetWithContext does not
// silently fall back to a local provider.
var ErrTunnelProviderUnavailable = fmt.Errorf("tunnel provider is registered but unavailable")

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

	// Fast path: return cached provider if the generation hasn't changed.
	r.mu.Lock()
	cached := r.cache[name]
	cachedGen := r.cacheGen[name]
	r.mu.Unlock()

	if cached != nil && cachedGen == reg.Generation {
		return cached, nil
	}

	// Slow path: use singleflight so concurrent calls for the same app name
	// share one proxy build, preventing duplicate gRPC connections and races
	// where one goroutine closes a provider another just received.
	val, err, _ := r.group.Do(name, func() (any, error) {
		// Re-check the cache under the singleflight token; another caller in
		// the same flight may have already built and cached the provider.
		r.mu.Lock()
		cached := r.cache[name]
		cachedGen := r.cacheGen[name]
		r.mu.Unlock()
		if cached != nil && cachedGen == reg.Generation {
			return cached, nil
		}

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
			return nil, fmt.Errorf("%w: build proxy for %s: %v", ErrTunnelProviderUnavailable, name, err)
		}

		// Install the new provider and close the previous one (if any). The
		// comparison is against the current cache entry, not the stale
		// pre-build read, so we always close exactly the entry being replaced.
		r.mu.Lock()
		old := r.cache[name]
		r.cache[name] = provider
		r.cacheGen[name] = reg.Generation
		r.mu.Unlock()

		if old != nil && old != provider {
			_ = old.Close()
		}

		return provider, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(core.Provider), nil
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
