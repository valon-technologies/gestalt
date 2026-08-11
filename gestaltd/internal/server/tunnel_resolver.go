package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coredata "github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/remotepublish"
)

type TunnelResolverConfig struct {
	RemoteRegistrations *coredata.RemoteRegistrationService
	ConnectAddr         string
	ClientIdentity      tls.Certificate
}

type tunnelProviderResolver struct {
	cfg TunnelResolverConfig
}

func newTunnelProviderResolver(cfg TunnelResolverConfig) *tunnelProviderResolver {
	if cfg.RemoteRegistrations == nil || strings.TrimSpace(cfg.ConnectAddr) == "" {
		return nil
	}
	return &tunnelProviderResolver{cfg: cfg}
}

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

	buildCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	provider, err := remotepublish.NewTunnelProxyProvider(buildCtx, remotepublish.TunnelProxyConfig{
		AppName:        name,
		StaticHeaders:  remotepublish.StaticHeadersFromDefinition(remoteProvider.Definition),
		TunnelHost:     reg.TunnelHost,
		PinnedSPKI:     reg.ServerSPKISHA256,
		ConnectAddr:    r.cfg.ConnectAddr,
		ClientIdentity: r.cfg.ClientIdentity,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: build proxy for %s: %v", ErrTunnelProviderUnavailable, name, err)
	}

	context.AfterFunc(ctx, func() { closeQuietly(provider) })

	return provider, nil
}

func (r *tunnelProviderResolver) HasRegistration(ctx context.Context, appName string) bool {
	if r == nil || r.cfg.RemoteRegistrations == nil {
		return false
	}
	_, _, err := r.cfg.RemoteRegistrations.ResolveProvider(ctx, "app", appName)
	return err == nil
}

func closeQuietly(p core.Provider) {
	if c, ok := p.(io.Closer); ok {
		_ = c.Close()
	}
}

var _ registry.RemoteResolver[core.Provider] = (*tunnelProviderResolver)(nil)
