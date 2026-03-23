package chat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/core"
)

func (b *Binding) listAgents(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}
	agents, err := b.chatStore.ListAgents(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, agents)
}

func (b *Binding) createAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}
	var req struct {
		Name         string   `json:"name"`
		SystemPrompt string   `json:"system_prompt"`
		Model        string   `json:"model"`
		Providers    []string `json:"providers"`
		Temperature  float64  `json:"temperature"`
		MaxTokens    int      `json:"max_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	agent := &core.Agent{
		ID:           uuid.NewString(),
		OwnerID:      userID,
		Name:         req.Name,
		SystemPrompt: req.SystemPrompt,
		Model:        req.Model,
		Providers:    req.Providers,
		Temperature:  req.Temperature,
		MaxTokens:    req.MaxTokens,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := b.chatStore.CreateAgent(r.Context(), agent); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, agent)
}

func (b *Binding) getAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}
	agent, err := b.chatStore.GetAgent(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if agent.OwnerID != userID {
		writeError(w, http.StatusNotFound, "agent not found")
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
	agent, err := b.chatStore.GetAgent(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if agent.OwnerID != userID {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	var req struct {
		Name         *string  `json:"name"`
		SystemPrompt *string  `json:"system_prompt"`
		Model        *string  `json:"model"`
		Providers    []string `json:"providers"`
		Temperature  *float64 `json:"temperature"`
		MaxTokens    *int     `json:"max_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name != nil {
		agent.Name = *req.Name
	}
	if req.SystemPrompt != nil {
		agent.SystemPrompt = *req.SystemPrompt
	}
	if req.Model != nil {
		agent.Model = *req.Model
	}
	if req.Providers != nil {
		agent.Providers = req.Providers
	}
	if req.Temperature != nil {
		agent.Temperature = *req.Temperature
	}
	if req.MaxTokens != nil {
		agent.MaxTokens = *req.MaxTokens
	}
	agent.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := b.chatStore.UpdateAgent(r.Context(), agent); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

func (b *Binding) deleteAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}
	id := chi.URLParam(r, "id")
	agent, err := b.chatStore.GetAgent(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if agent.OwnerID != userID {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if err := b.chatStore.DeleteAgent(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	convs, err := b.chatStore.ListConversations(r.Context(), userID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, convs)
}

func (b *Binding) createConversation(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}
	var req struct {
		AgentID string `json:"agent_id"`
		Title   string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	conv := &core.Conversation{
		ID:        uuid.NewString(),
		AgentID:   req.AgentID,
		UserID:    userID,
		Title:     req.Title,
		Status:    core.ConversationActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := b.chatStore.CreateConversation(r.Context(), conv); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, conv)
}

func (b *Binding) getConversation(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}
	conv, err := b.chatStore.GetConversation(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if conv.UserID != userID {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

func (b *Binding) listMessages(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing X-User-ID header")
		return
	}
	convID := chi.URLParam(r, "id")
	conv, err := b.chatStore.GetConversation(r.Context(), convID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if conv.UserID != userID {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	msgs, err := b.chatStore.ListMessages(r.Context(), convID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if b.dispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "chat dispatcher not available")
		return
	}

	events, err := b.dispatcher.SendMessage(r.Context(), convID, req.Content, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	for event := range events {
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
		flusher.Flush()
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
