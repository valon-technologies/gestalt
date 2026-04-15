package server

import "github.com/go-chi/chi/v5"

func (s *Server) mountAuthenticatedRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Get("/integrations", s.listIntegrations)
		r.Delete("/integrations/{name}", s.disconnectIntegration)
		r.Get("/integrations/{name}/operations", s.listOperations)

		r.Get("/{integration}/{operation}", s.executeOperation)
		r.Post("/{integration}/{operation}", s.executeOperation)

		r.Post("/auth/start-oauth", s.startIntegrationOAuth)
		r.Post("/auth/connect-manual", s.connectManual)

		r.Post("/tokens", s.createAPIToken)
		r.Get("/tokens", s.listAPITokens)
		r.Delete("/tokens", s.revokeAllAPITokens)
		r.Delete("/tokens/{id}", s.revokeAPIToken)

		r.Route("/identities", func(r chi.Router) {
			r.Get("/", s.listManagedIdentities)
			r.Post("/", s.createManagedIdentity)
			r.Get("/{identityID}", s.getManagedIdentityOrExecuteIdentitiesOperation)
			r.Post("/{identityID}", s.executeIdentitiesOperation)
			r.Patch("/{identityID}", s.updateManagedIdentityOrExecuteIdentitiesOperation)
			r.Delete("/{identityID}", s.deleteManagedIdentityOrExecuteIdentitiesOperation)
			r.Get("/{identityID}/members", s.listManagedIdentityMembers)
			r.Put("/{identityID}/members", s.putManagedIdentityMember)
			r.Delete("/{identityID}/members/{email}", s.deleteManagedIdentityMember)
			r.Get("/{identityID}/grants", s.listManagedIdentityGrants)
			r.Put("/{identityID}/grants/{plugin}", s.putManagedIdentityGrant)
			r.Delete("/{identityID}/grants/{plugin}", s.deleteManagedIdentityGrant)
			r.Get("/{identityID}/tokens", s.listManagedIdentityTokens)
			r.Post("/{identityID}/tokens", s.createManagedIdentityToken)
			r.Delete("/{identityID}/tokens/{id}", s.revokeManagedIdentityToken)
		})
	})
}
