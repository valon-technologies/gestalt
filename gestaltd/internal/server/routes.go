package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func (s *Server) connectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect && s.frpsConnectHandler != nil {
			s.frpsConnectHandler.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	r := s.router
	r.Use(clientKindTelemetryMiddleware)
	r.Use(requestMetaMiddleware)
	r.Use(routePatternTelemetryMiddleware)
	r.Use(s.securityHeadersMiddleware)
	r.Use(s.publicGRPCMiddleware)
	r.Use(s.hostServiceRelayMiddleware)
	r.Use(s.egressProxyMiddleware)
	r.Use(s.connectMiddleware)
	r.Use(maxBodyMiddleware(defaultMaxBodyBytes))

	switch s.routeProfile {
	case RouteProfilePublic:
		s.mountCoreRoutes(r, metricsHidden)
		s.mountSCIMRoutes(r)
		s.mountMCPRoutes(r)
		s.mountHTTPBindingRoutes(r)
		s.mountS3ObjectAccessRoutes(r)
		s.mountAPIRoutes(r)
		s.mountAdminAPIRoutes(r)
		s.mountAdminPageRedirects(r)
		s.mountMountedUIRoutes(r)
		s.mountManagementHiddenRoutes(r)
	case RouteProfileManagement:
		s.mountCoreRoutes(r, metricsUnauthenticated)
		s.mountManagementRootRedirect(r)
		s.mountAdminAPIRoutes(r)
		s.mountAdminPageRedirects(r)
		s.mountActivateRoute(r)
	default:
		s.mountCoreRoutes(r, metricsAuthenticated)
		s.mountSCIMRoutes(r)
		s.mountMCPRoutes(r)
		s.mountHTTPBindingRoutes(r)
		s.mountS3ObjectAccessRoutes(r)
		s.mountAPIRoutes(r)
		s.mountAdminAPIRoutes(r)
		s.mountAdminPageRedirects(r)
		s.mountMountedUIRoutes(r)
		s.mountActivateRoute(r)
	}
}

func (s *Server) mountSCIMRoutes(r chi.Router) {
	if s.scimHandler == nil {
		return
	}
	r.Handle("/scim/v2/*", s.scimHandler)
	r.Handle("/scim/v2", s.scimHandler)
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
	if s.frpsHandler != nil {
		r.Handle("/~!frp", s.frpsHandler)
	}

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
	if s.adminProductBase() == "" {
		return
	}
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		redirectPreservingQuery(w, r, s.adminProductURL("/admin"), http.StatusMovedPermanently)
	})
}

func (s *Server) mountAdminPageRedirects(r chi.Router) {
	if s.routeProfile == RouteProfileManagement && s.adminProductBase() == "" {
		return
	}
	r.Get("/admin/registry", func(w http.ResponseWriter, r *http.Request) {
		redirectPreservingQuery(w, r, s.adminProductURL("/admin/versions"), http.StatusMovedPermanently)
	})
	r.Get("/admin/registry/{app}", func(w http.ResponseWriter, r *http.Request) {
		app := strings.TrimSpace(chi.URLParam(r, "app"))
		redirectPreservingQuery(w, r, s.adminProductURL("/admin/versions/"+url.PathEscape(app)), http.StatusMovedPermanently)
	})
	if s.routeProfile == RouteProfileManagement {
		r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
			redirectPreservingQuery(w, r, s.adminProductURL("/admin"), http.StatusMovedPermanently)
		})
		r.Get("/admin/", func(w http.ResponseWriter, r *http.Request) {
			redirectPreservingQuery(w, r, s.adminProductURL("/admin"), http.StatusMovedPermanently)
		})
		r.Get("/admin/metrics", func(w http.ResponseWriter, r *http.Request) {
			redirectPreservingQuery(w, r, s.adminProductURL("/admin/metrics"), http.StatusMovedPermanently)
		})
		r.Get("/admin/versions", func(w http.ResponseWriter, r *http.Request) {
			redirectPreservingQuery(w, r, s.adminProductURL("/admin/versions"), http.StatusMovedPermanently)
		})
		r.Get("/admin/versions/{app}", func(w http.ResponseWriter, r *http.Request) {
			app := strings.TrimSpace(chi.URLParam(r, "app"))
			redirectPreservingQuery(w, r, s.adminProductURL("/admin/versions/"+url.PathEscape(app)), http.StatusMovedPermanently)
		})
	}
}

func (s *Server) adminProductBase() string {
	if s.routeProfile != RouteProfileManagement {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(s.publicBaseURL), "/")
}

func (s *Server) adminProductURL(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if base := s.adminProductBase(); base != "" {
		return base + path
	}
	return path
}

func (s *Server) mountAdminAPIRoutes(r chi.Router) {
	r.Route("/admin/api/v1", func(r chi.Router) {
		r.Use(s.adminAPIAuthMiddleware)
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			s.mountAdminRuntimeRoutes(r)
			s.mountAdminAppRegistryRoutes(r)
			s.mountAdminAppInstallReadRoutes(r)
			s.mountAdminAppRolloutRoutes(r)
			s.mountAdminMetricsRoutes(r)
			s.mountAdminPlatformAdminsRoutes(r)
		})
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(10 * time.Minute))
			s.mountAdminAppInstallWriteRoutes(r)
		})
	})
}

func (s *Server) mountMountedUIRoutes(r chi.Router) {
	var rootHandler http.Handler
	for i := range s.mountedUIs {
		mounted := &s.mountedUIs[i]
		if mounted.Handler == nil || mounted.Path == "" {
			continue
		}
		path := mounted.Path
		handler := s.mountedUIHandler(*mounted)
		if path == "/" {
			rootHandler = handler
			continue
		}
		r.Get(path, ingressKindTelemetryHandler(metricutil.IngressKindMountedUI, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			redirectPreservingQuery(w, r, path+"/", http.StatusMovedPermanently)
		})))
		r.Handle(path+"/*", ingressKindTelemetryHandler(metricutil.IngressKindMountedUI, handler))
	}
	if rootHandler != nil {
		r.NotFound(ingressKindTelemetryHandler(metricutil.IngressKindMountedUI, rootHandler).ServeHTTP)
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
	r.Handle("/activate", notFound)
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
	r.Handle(mcpPath, s.mcpEndpointHandler())
}

func (s *Server) mountAPIRoutes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(s.apiRouteTimeout))
			s.mountAuthRoutes(r)
			s.mountAuthenticatedRoutes(r)
		})
		r.NotFound(apiNotFound)
		r.MethodNotAllowed(apiMethodNotAllowed)
	})
	s.mountPublicRESTRoutes(r)
}

func (s *Server) mountPublicRESTRoutes(r chi.Router) {
	r.Route("/api/v2", func(r chi.Router) {
		if s.publicRESTHandler == nil {
			r.NotFound(apiNotFound)
			r.MethodNotAllowed(apiMethodNotAllowed)
			return
		}
		r.Handle("/*", ingressKindTelemetryHandler(metricutil.IngressKindPublicREST, s.publicRESTHandler))
		r.NotFound(apiNotFound)
		r.MethodNotAllowed(apiMethodNotAllowed)
	})
}

const defaultAPIRouteTimeout = 60 * time.Second

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
