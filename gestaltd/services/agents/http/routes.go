package agentshttp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	CreateSession      http.HandlerFunc
	ListSessions       http.HandlerFunc
	ListProviders      http.HandlerFunc
	GetSession         http.HandlerFunc
	UpdateSession      http.HandlerFunc
	CreateTurn         http.HandlerFunc
	ListTurns          http.HandlerFunc
	GetTurn            http.HandlerFunc
	CancelTurn         http.HandlerFunc
	ListTurnEvents     http.HandlerFunc
	StreamTurnEvents   http.HandlerFunc
	ListInteractions   http.HandlerFunc
	ResolveInteraction http.HandlerFunc
}

func Mount(r chi.Router, h Handlers) {
	r.Post("/agent/sessions", h.CreateSession)
	r.Get("/agent/sessions", h.ListSessions)
	r.Get("/agent/providers", h.ListProviders)
	r.Route("/agent/sessions", func(r chi.Router) {
		r.Post("/", h.CreateSession)
		r.Get("/", h.ListSessions)
		r.Get("/{sessionID}", h.GetSession)
		r.Patch("/{sessionID}", h.UpdateSession)
		r.Post("/{sessionID}/turns", h.CreateTurn)
		r.Get("/{sessionID}/turns", h.ListTurns)
	})
	r.Route("/agent/turns", func(r chi.Router) {
		r.Get("/{turnID}", h.GetTurn)
		r.Post("/{turnID}/cancel", h.CancelTurn)
		r.Get("/{turnID}/events", h.ListTurnEvents)
		r.Get("/{turnID}/events/stream", h.StreamTurnEvents)
		r.Get("/{turnID}/interactions", h.ListInteractions)
		r.Post("/{turnID}/interactions/{interactionID}/resolve", h.ResolveInteraction)
	})
}
