package providerdevhttp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	CreateAttachment http.HandlerFunc
	ListAttachments  http.HandlerFunc
	GetAttachment    http.HandlerFunc
}

func Mount(r chi.Router, h Handlers) {
	r.Post("/provider-dev/attachments", h.CreateAttachment)
	r.Get("/provider-dev/attachments", h.ListAttachments)
	r.Get("/provider-dev/attachments/{attachmentID}", h.GetAttachment)
}
