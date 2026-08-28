package server

import (
	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func (s *Server) mountAuthenticatedRoutes(r chi.Router) {
	s.mountAppAdminRegistryRoutes(r)
	s.mountAppAdminMembersRoutes(r)
	s.mountAppAdminIdentitiesRoutes(r)
	s.mountAppAdminMetricsRoutes(r)

	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Get("/catalog/apps", s.listAppCatalog)
		r.Get("/catalog/apps/{name}/icon", s.serveAppCatalogIcon)
		r.Get("/me/app-connections", s.listAppConnections)

		r.Get("/apps", s.listIntegrations)
		r.Delete("/apps/{name}", s.disconnectIntegration)
		r.Put("/apps/{name}/preferred-instance", s.selectPreferredInstance)
		r.Get("/apps/{name}/access", s.getAppAccess)
		r.Put("/apps/{name}/access", s.updateAppAccess)

		r.Post("/workflow/events", s.deliverWorkflowEvent)

		r.Get("/auth/session", s.authSession)
		r.Post("/auth/start-oauth", s.startIntegrationOAuth)
		r.Post("/auth/connect-manual", s.connectManual)

		r.Post("/tokens", s.createAPIToken)
		r.Delete("/tokens/{id}", s.revokeAPIToken)

	})

	r.With(s.pluginRouteAuthMiddleware("name", false)).Get("/apps/{name}/operations", s.listOperations)
	r.Group(func(r chi.Router) {
		r.Use(s.uiAPIIngressTelemetryMiddleware(metricutil.IngressKindAppInvokeV1))
		r.Use(s.pluginRouteAuthMiddleware("integration", true))
		r.Get("/{integration}/{operation}", s.executeOperation)
		r.Post("/{integration}/{operation}", s.executeOperation)
	})
}
