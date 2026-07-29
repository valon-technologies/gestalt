package remotepublish

import (
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/grpcutil"
	"google.golang.org/grpc"
)

// newTunnelDispatchHandler returns an http.Handler that dispatches gRPC
// requests (by Content-Type) to the gRPC server and all other requests to the
// UI handler. This mirrors the main server's publicGRPCMiddleware pattern.
func newTunnelDispatchHandler(grpcServer *grpc.Server, uiHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if grpcutil.IsGRPCRequest(r) {
			grpcServer.ServeHTTP(w, r)
			return
		}
		if uiHandler != nil {
			uiHandler.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

// newTunnelUIHandler builds an http.Handler that serves static UI assets for
// the apps in a publication group. It uses the dev handler resolver (for
// dev-mode apps) or falls back to 404 for apps without a local static handler.
// The remote gestaltd proxies UI requests through the tunnel to this handler.
func newTunnelUIHandler(devHandlerResolver func(string) http.Handler, providers []ProviderPublication) http.Handler {
	providerNames := make(map[string]bool, len(providers))
	for _, p := range providers {
		providerNames[p.Name] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appName := tunnelUIAppFromPath(r.URL.Path)
		if appName == "" || !providerNames[appName] {
			http.NotFound(w, r)
			return
		}
		var handler http.Handler
		if devHandlerResolver != nil {
			handler = devHandlerResolver(appName)
		}
		if handler == nil {
			http.NotFound(w, r)
			return
		}
		// Strip the /ui/{app} prefix before delegating to the app's static
		// handler, so the handler sees paths relative to its mount root.
		rest := strings.TrimPrefix(r.URL.Path, "/ui/"+appName)
		if rest == "" {
			rest = "/"
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = rest
		handler.ServeHTTP(w, r2)
	})
}

// tunnelUIAppFromPath extracts the app name from a /ui/{app}/... path. Returns
// empty string if the path does not match this pattern.
func tunnelUIAppFromPath(path string) string {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/ui/") {
		return ""
	}
	rest := strings.TrimPrefix(path, "/ui/")
	if rest == "" {
		return ""
	}
	// Take the first path segment as the app name.
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}
