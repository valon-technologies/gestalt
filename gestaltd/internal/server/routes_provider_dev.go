package server

import "github.com/go-chi/chi/v5"

func (s *Server) mountProviderDevPublicRoutes(r chi.Router) {
	r.Post("/provider-dev/attach-authorizations", s.createProviderDevAttachAuthorization)
	r.Get("/provider-dev/attach-authorizations/{authorizationID}", s.showProviderDevAttachAuthorization)
	r.Post("/provider-dev/attach-authorizations/{authorizationID}/approve", s.approveProviderDevAttachAuthorization)
	r.Get("/provider-dev/attach-authorizations/{authorizationID}/poll", s.pollProviderDevAttachAuthorization)
	r.Post("/provider-dev/attach-authorizations/{authorizationID}/attachments", s.createAuthorizedProviderDevSession)

	r.Get("/provider-dev/attachments/{attachmentID}/poll", s.pollProviderDevSessionByDispatcher)
	r.Post("/provider-dev/attachments/{attachmentID}/calls/{callID}", s.completeProviderDevCallByDispatcher)
	r.Delete("/provider-dev/attachments/{attachmentID}", s.closeProviderDevSessionByDispatcherOrOwner)
}
