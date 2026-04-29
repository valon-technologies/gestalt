package identityhttp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	StartOAuth         http.HandlerFunc
	ConnectManual      http.HandlerFunc
	CreateAPIToken     http.HandlerFunc
	ListAPITokens      http.HandlerFunc
	RevokeAllAPITokens http.HandlerFunc
	RevokeAPIToken     http.HandlerFunc
}

func Mount(r chi.Router, h Handlers) {
	r.Post("/auth/start-oauth", h.StartOAuth)
	r.Post("/auth/connect-manual", h.ConnectManual)

	r.Post("/tokens", h.CreateAPIToken)
	r.Get("/tokens", h.ListAPITokens)
	r.Delete("/tokens", h.RevokeAllAPITokens)
	r.Delete("/tokens/{id}", h.RevokeAPIToken)
}
