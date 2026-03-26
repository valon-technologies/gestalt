package server

import "github.com/go-chi/chi/v5"

func (s *Server) mountAdminRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Use(s.adminMiddleware)

		r.Post("/egress/deny-rules", s.createEgressDenyRule)
		r.Get("/egress/deny-rules", s.listEgressDenyRules)
		r.Delete("/egress/deny-rules/{id}", s.deleteEgressDenyRule)

		r.Post("/egress/credential-grants", s.createEgressCredentialGrant)
		r.Get("/egress/credential-grants", s.listEgressCredentialGrants)
		r.Delete("/egress/credential-grants/{id}", s.deleteEgressCredentialGrant)
	})
}
