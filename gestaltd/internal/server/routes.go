package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (s *Server) routes() {
	r := s.router
	r.Use(requestMetaMiddleware)
	r.Use(s.securityHeadersMiddleware)
	r.Use(maxBodyMiddleware(1 << 20)) // 1 MB

	switch s.routeProfile {
	case RouteProfilePublic:
		s.mountCoreRoutes(r, metricsHidden)
		s.mountMCPRoutes(r)
		s.mountAPIRoutes(r)
		s.mountManagementHiddenRoutes(r)
	case RouteProfileManagement:
		s.mountCoreRoutes(r, metricsUnauthenticated)
		s.mountManagementRootRedirect(r)
		s.mountAdminUIRoutes(r)
	default:
		s.mountCoreRoutes(r, metricsAuthenticated)
		s.mountMCPRoutes(r)
		s.mountAPIRoutes(r)
		s.mountAdminUIRoutes(r)
	}

	if s.clientUI != nil {
		r.NotFound(s.clientUI.ServeHTTP)
	}
}

type metricsExposure int

const (
	metricsHidden metricsExposure = iota
	metricsAuthenticated
	metricsUnauthenticated
)

func (s *Server) mountCoreRoutes(r chi.Router, exposure metricsExposure) {
	r.Get("/health", s.healthCheck)
	r.Get("/ready", s.readinessCheck)
	switch exposure {
	case metricsAuthenticated:
		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)
			r.HandleFunc("/metrics", s.servePrometheusMetrics)
		})
	case metricsUnauthenticated:
		r.HandleFunc("/metrics", s.servePrometheusMetrics)
	}
}

func (s *Server) mountManagementRootRedirect(r chi.Router) {
	if s.adminUI == nil {
		return
	}

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
}

func (s *Server) mountAdminUIRoutes(r chi.Router) {
	if s.adminUI == nil {
		return
	}

	r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	r.Handle("/admin/*", http.StripPrefix("/admin", s.adminUI))
}

func (s *Server) mountManagementHiddenRoutes(r chi.Router) {
	notFound := http.NotFoundHandler()
	r.Handle("/metrics", notFound)
	r.Handle("/admin", notFound)
	r.Handle("/admin/*", notFound)
}

func (s *Server) mountMCPRoutes(r chi.Router) {
	if s.mcpHandler == nil {
		return
	}
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Handle("/mcp", s.mcpHandler)
	})
}

func (s *Server) mountAPIRoutes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))
		s.mountAuthRoutes(r)
		s.mountAuthenticatedRoutes(r)
	})
}

// BuildPluginRouter creates a scoped chi.Router for a plugin's subdomain.
// It reuses the server's auth, middleware, and handlers, scoping API routes
// to the given plugin name.
func (s *Server) BuildPluginRouter(pluginName string, staticHandler, mcpHandler http.Handler, allowedUsers []string) chi.Router {
	r := chi.NewRouter()
	r.Use(requestMetaMiddleware)
	r.Use(s.securityHeadersMiddleware)
	r.Use(maxBodyMiddleware(1 << 20))

	r.Get("/health", s.healthCheck)
	r.Get("/ready", s.readinessCheck)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))
		s.mountAuthRoutes(r)
		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)
			if len(allowedUsers) > 0 {
				r.Use(authzMiddleware(allowedUsers))
			}
			r.Use(scopeIntegration(pluginName))

			r.Get("/integrations", s.listIntegrations)
			r.Get("/operations", s.listOperations)
			r.Get("/{operation}", s.executeOperation)
			r.Post("/{operation}", s.executeOperation)

			r.Post("/auth/start-oauth", s.startIntegrationOAuth)
			r.Post("/auth/connect-manual", s.connectManual)
		})
	})

	if mcpHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)
			if len(allowedUsers) > 0 {
				r.Use(authzMiddleware(allowedUsers))
			}
			r.Handle("/mcp", mcpHandler)
		})
	}

	if staticHandler != nil {
		r.NotFound(staticHandler.ServeHTTP)
	}

	return r
}

func (s *Server) servePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if s.prometheusMetrics == nil {
		http.Error(w, "Prometheus metrics are unavailable because telemetry metrics are disabled.", http.StatusServiceUnavailable)
		return
	}
	s.prometheusMetrics.ServeHTTP(w, r)
}
