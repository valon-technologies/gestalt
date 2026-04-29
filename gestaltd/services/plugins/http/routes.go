package pluginshttp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Middleware = func(http.Handler) http.Handler

type AuthenticatedHandlers struct {
	ListIntegrations      http.HandlerFunc
	DisconnectIntegration http.HandlerFunc
}

type OperationHandlers struct {
	ListOperations            http.HandlerFunc
	ExecuteOperation          http.HandlerFunc
	PluginRouteAuthMiddleware func(param string) Middleware
}

func MountAuthenticated(r chi.Router, h AuthenticatedHandlers) {
	r.Get("/integrations", h.ListIntegrations)
	r.Delete("/integrations/{name}", h.DisconnectIntegration)
}

func MountOperations(r chi.Router, h OperationHandlers) {
	r.With(h.PluginRouteAuthMiddleware("name")).Get("/integrations/{name}/operations", h.ListOperations)
	r.With(h.PluginRouteAuthMiddleware("integration")).Get("/{integration}/{operation}", h.ExecuteOperation)
	r.With(h.PluginRouteAuthMiddleware("integration")).Post("/{integration}/{operation}", h.ExecuteOperation)
}
