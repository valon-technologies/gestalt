package agentic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/sse"
)

var _ core.Binding = (*Binding)(nil)

type bindingConfig struct {
	Runtime string `yaml:"runtime"`
}

type Binding struct {
	name       string
	store      Store
	dispatcher Dispatcher
}

func NewBinding(name string, store Store, dispatcher Dispatcher) *Binding {
	return &Binding{name: name, store: store, dispatcher: dispatcher}
}

func (b *Binding) Name() string                  { return b.name }
func (b *Binding) Kind() core.BindingKind        { return core.BindingSurface }
func (b *Binding) Start(_ context.Context) error { return nil }
func (b *Binding) Close() error                  { return nil }

func (b *Binding) Routes() []core.Route {
	return []core.Route{
		{Method: http.MethodGet, Pattern: "/agents", Handler: http.HandlerFunc(b.listAgents)},
		{Method: http.MethodPost, Pattern: "/agents", Handler: http.HandlerFunc(b.createAgent)},
		{Method: http.MethodGet, Pattern: "/agents/{id}", Handler: http.HandlerFunc(b.getAgent)},
		{Method: http.MethodPut, Pattern: "/agents/{id}", Handler: http.HandlerFunc(b.updateAgent)},
		{Method: http.MethodDelete, Pattern: "/agents/{id}", Handler: http.HandlerFunc(b.deleteAgent)},
		{Method: http.MethodGet, Pattern: "/conversations", Handler: http.HandlerFunc(b.listConversations)},
		{Method: http.MethodPost, Pattern: "/conversations", Handler: http.HandlerFunc(b.createConversation)},
		{Method: http.MethodGet, Pattern: "/conversations/{id}", Handler: http.HandlerFunc(b.getConversation)},
		{Method: http.MethodGet, Pattern: "/conversations/{id}/messages", Handler: http.HandlerFunc(b.listMessages)},
		{Method: http.MethodPost, Pattern: "/conversations/{id}/messages", Handler: http.HandlerFunc(b.sendMessage)},
	}
}

func (b *Binding) listAgents(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}

	agents, err := b.store.ListAgents(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	if agents == nil {
		agents = []*Agent{}
	}
	writeJSON(w, http.StatusOK, agents)
}

func (b *Binding) createAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}

	var agent Agent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agent.OwnerID = userID
	if agent.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if err := b.store.CreateAgent(r.Context(), &agent); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent")
		return
	}
	writeJSON(w, http.StatusCreated, agent)
}

func (b *Binding) getAgent(w http.ResponseWriter, r *http.Request) {
	agent, err := b.store.GetAgent(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent")
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

func (b *Binding) updateAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}

	id := chi.URLParam(r, "id")
	existing, err := b.store.GetAgent(r.Context(), id)
	if errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent")
		return
	}
	if existing.OwnerID != userID {
		writeError(w, http.StatusForbidden, "not the owner of this agent")
		return
	}

	var update Agent
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	update.ID = id
	update.OwnerID = existing.OwnerID
	update.CreatedAt = existing.CreatedAt

	if err := b.store.UpdateAgent(r.Context(), &update); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agent")
		return
	}
	writeJSON(w, http.StatusOK, update)
}

func (b *Binding) deleteAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}

	id := chi.URLParam(r, "id")
	existing, err := b.store.GetAgent(r.Context(), id)
	if errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent")
		return
	}
	if existing.OwnerID != userID {
		writeError(w, http.StatusForbidden, "not the owner of this agent")
		return
	}

	if err := b.store.DeleteAgent(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete agent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (b *Binding) listConversations(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	convs, err := b.store.ListConversations(r.Context(), userID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list conversations")
		return
	}
	if convs == nil {
		convs = []*Conversation{}
	}
	writeJSON(w, http.StatusOK, convs)
}

func (b *Binding) createConversation(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}

	var conv Conversation
	if err := json.NewDecoder(r.Body).Decode(&conv); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if conv.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	agent, err := b.store.GetAgent(r.Context(), conv.AgentID)
	if errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "agent not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate agent")
		return
	}
	_ = agent

	conv.UserID = userID
	if conv.Title == "" {
		conv.Title = "New conversation"
	}

	if err := b.store.CreateConversation(r.Context(), &conv); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create conversation")
		return
	}
	writeJSON(w, http.StatusCreated, conv)
}

func (b *Binding) getConversation(w http.ResponseWriter, r *http.Request) {
	conv, err := b.store.GetConversation(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get conversation")
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

func (b *Binding) listMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := b.store.GetConversation(r.Context(), id)
	if errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get conversation")
		return
	}

	msgs, err := b.store.ListMessages(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	if msgs == nil {
		msgs = []*Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (b *Binding) sendMessage(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}

	convID := chi.URLParam(r, "id")
	conv, err := b.store.GetConversation(r.Context(), convID)
	if errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get conversation")
		return
	}
	if conv.UserID != userID {
		writeError(w, http.StatusForbidden, "not the owner of this conversation")
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	sw, err := sse.NewWriter(w)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	events, err := b.dispatcher.SendMessage(r.Context(), convID, body.Content, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send message")
		return
	}

	for ev := range events {
		_ = sw.WriteJSON(string(ev.Type), ev)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
