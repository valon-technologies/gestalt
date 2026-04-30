package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
)

func (s *Server) routes() {
	r := s.router
	r.Use(requestMetaMiddleware)
	r.Use(routePatternTelemetryMiddleware)
	r.Use(s.securityHeadersMiddleware)
	r.Use(s.hostServiceRelayMiddleware)
	r.Use(s.egressProxyMiddleware)
	r.Use(maxBodyMiddleware(defaultMaxBodyBytes))

	switch s.routeProfile {
	case RouteProfilePublic:
		s.mountCoreRoutes(r, metricsHidden)
		s.mountMCPRoutes(r)
		s.mountHTTPBindingRoutes(r)
		s.mountS3ObjectAccessRoutes(r)
		s.mountAPIRoutes(r)
		s.mountMountedUIRoutes(r)
		s.mountManagementHiddenRoutes(r)
	case RouteProfileManagement:
		s.mountCoreRoutes(r, metricsUnauthenticated)
		s.mountManagementRootRedirect(r)
		s.mountAdminAPIRoutes(r)
		s.mountAdminUIRoutes(r)
	default:
		s.mountCoreRoutes(r, metricsAuthenticated)
		s.mountMCPRoutes(r)
		s.mountHTTPBindingRoutes(r)
		s.mountS3ObjectAccessRoutes(r)
		s.mountAPIRoutes(r)
		s.mountMountedUIRoutes(r)
		s.mountAdminAPIRoutes(r)
		s.mountAdminUIRoutes(r)
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
		redirectPreservingQuery(w, r, "/admin/", http.StatusMovedPermanently)
	})
}

func (s *Server) mountAdminUIRoutes(r chi.Router) {
	if s.adminUI == nil {
		return
	}

	r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		redirectPreservingQuery(w, r, "/admin/", http.StatusMovedPermanently)
	})
	r.Handle("/admin/*", s.adminUIHandler())
}

func (s *Server) mountAdminAPIRoutes(r chi.Router) {
	r.Route("/admin/api/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))
		if s.adminRoute.AuthorizationPolicy != "" {
			r.Use(s.adminAPIAuthMiddleware)
		}
		s.mountAdminRuntimeRoutes(r)
		s.mountAdminAuthorizationRoutes(r)
	})
}

func (s *Server) mountMountedUIRoutes(r chi.Router) {
	var rootHandler http.Handler
	for _, mounted := range s.mountedUIs {
		if mounted.Handler == nil || mounted.Path == "" {
			continue
		}
		path := mounted.Path
		handler := s.mountedUIHandler(mounted)
		if path == "/" {
			rootHandler = handler
			continue
		}
		r.Get(path, func(w http.ResponseWriter, r *http.Request) {
			redirectPreservingQuery(w, r, path+"/", http.StatusMovedPermanently)
		})
		r.Handle(path+"/*", handler)
	}
	if rootHandler != nil {
		r.NotFound(rootHandler.ServeHTTP)
	}
}

func redirectPreservingQuery(w http.ResponseWriter, r *http.Request, target string, code int) {
	if rawQuery := r.URL.RawQuery; rawQuery != "" {
		target += "?" + rawQuery
	}
	http.Redirect(w, r, target, code)
}

func routePatternTelemetryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		routeCtx := chi.RouteContext(r.Context())
		if routeCtx == nil {
			return
		}
		if route := routeCtx.RoutePattern(); route != "" {
			metricutil.AddHTTPAttributes(r.Context(), metricutil.AttrHTTPRoute.String(route))
		}
	})
}

func (s *Server) mountManagementHiddenRoutes(r chi.Router) {
	notFound := http.NotFoundHandler()
	r.Handle("/metrics", notFound)
	r.Handle("/admin/api/v1", notFound)
	r.Handle("/admin/api/v1/*", notFound)
	r.Handle("/admin", notFound)
	r.Handle("/admin/*", notFound)
}

func (s *Server) mountMCPRoutes(r chi.Router) {
	if s.mcpHandler == nil {
		return
	}
	r.Get(mcpProtectedResourceMetadataPath, s.mcpProtectedResourceMetadata)
	r.Get(mcpAuthorizationServerMetadataPath, s.mcpAuthorizationServerMetadata)
	r.Get(mcpAuthorizationServerMetadataMCPPath, s.mcpAuthorizationServerMetadata)
	r.Post(mcpRegistrationEndpointPath, s.mcpRegisterOAuthClient)
	r.Get(mcpAuthorizationEndpointPath, s.mcpOAuthAuthorize)
	r.Post(mcpTokenEndpointPath, s.mcpOAuthToken)
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Handle(mcpPath, s.mcpHandler)
	})
}

func (s *Server) mountAPIRoutes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))
		s.mountAuthRoutes(r)
		s.mountProviderDevPublicRoutes(r)
		s.mountAuthenticatedRoutes(r)
		r.NotFound(apiNotFound)
		r.MethodNotAllowed(apiMethodNotAllowed)
	})
}

func apiNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, fmt.Sprintf("API route %q not found", r.URL.Path))
}

func apiMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s is not allowed for API route %q", r.Method, r.URL.Path))
}

func (s *Server) servePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if s.prometheusMetrics == nil {
		http.Error(w, "Prometheus metrics are unavailable because telemetry metrics are disabled.", http.StatusServiceUnavailable)
		return
	}
	s.prometheusMetrics.ServeHTTP(w, r)
}
