package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/tunnel"
)

// tunnelUIProxyHandler returns an http.Handler that reverse-proxies UI requests
// through the tunnel when a tunnel registration exists for the app. If no
// registration exists, the fallback handler is used instead.
//
// The proxy rewrites the request path to /ui/{app}/... so the tunnel's
// newTunnelUIHandler can route to the correct app's static assets.
func (s *Server) tunnelUIProxyHandler(appName, mountPath string, fallback http.Handler) http.Handler {
	if s == nil || s.tunnelResolver == nil {
		return fallback
	}
	proxy := newTunnelUIReverseProxy(s.tunnelResolver, appName, mountPath)
	if proxy == nil {
		return fallback
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hasTunnelRegistration(r.Context(), appName) {
			fallback.ServeHTTP(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// hasTunnelRegistration checks whether a tunnel registration exists for the
// given app name.
func (s *Server) hasTunnelRegistration(ctx context.Context, appName string) bool {
	if s == nil || s.tunnelResolver == nil {
		return false
	}
	_, _, err := s.tunnelResolver.cfg.RemoteRegistrations.ResolveProvider(ctx, "app", appName)
	return err == nil
}

// newTunnelUIReverseProxy creates a reverse proxy that forwards requests
// through the tunnel dialer to the tunnel's HTTP UI handler. Returns nil if
// the tunnel resolver is not configured.
func newTunnelUIReverseProxy(resolver *tunnelProviderResolver, appName, mountPath string) *httputil.ReverseProxy {
	if resolver == nil || strings.TrimSpace(appName) == "" {
		return nil
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			_, reg, err := resolver.cfg.RemoteRegistrations.ResolveProvider(ctx, "app", appName)
			if err != nil || reg == nil {
				return nil, &tunnelProxyError{"no tunnel registration for app " + appName}
			}
			dialer := tunnel.NewDialer(tunnel.DialerConfig{
				ConnectAddr:    resolver.cfg.ConnectAddr,
				TunnelHost:     reg.TunnelHost,
				PinnedSPKI:     reg.ServerSPKISHA256,
				ClientIdentity: resolver.cfg.ClientIdentity,
			})
			return dialer.DialContext(ctx, "tcp", "")
		},
	}

	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			rest := strings.TrimPrefix(r.URL.Path, strings.TrimRight(mountPath, "/"))
			if rest == "" {
				rest = "/"
			}
			r.URL.Path = "/ui/" + appName + rest
			r.Host = appName
		},
		Transport: transport,
	}
	return proxy
}

type tunnelProxyError struct{ msg string }

func (e *tunnelProxyError) Error() string { return e.msg }
