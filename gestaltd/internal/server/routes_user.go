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

	s.mountCatalogCLIRoutes(r)

	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Get("/me/app-connections", s.listAppConnections)

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

		r.Post("/authorization/subjects", s.createAuthorizationSubject)
		r.Post("/authorization/subjects/{subjectId}/tokens", s.createAuthorizationSubjectToken)
		r.Put("/authorization/subjects/{subjectId}/grants", s.setAuthorizationSubjectGrant)

	})

	r.With(s.cliOptionalSubjectLabelMiddleware("name"), s.pluginRouteAuthMiddleware("name")).Get("/apps/{name}/operations", s.listOperations)
	r.Group(func(r chi.Router) {
		r.Use(s.uiAPIIngressTelemetryMiddleware(metricutil.IngressKindAppInvokeV1))
		r.Use(subjectLabelRecorderMiddleware)
		r.Use(s.pluginRouteAuthWithSubjectLabelMiddleware("integration"))
		r.Get("/{integration}/{operation}", s.executeOperation)
		r.Post("/{integration}/{operation}", s.executeOperation)
	})
}

func (s *Server) mountCatalogCLIRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.cliOptionalSubjectLabelMiddleware(""))
		r.Use(s.authMiddleware)

		r.Get("/catalog/apps", s.listAppCatalog)
		r.Get("/catalog/apps/{name}/icon", s.serveAppCatalogIcon)
		r.Get("/apps", s.listIntegrations)
	})
}
