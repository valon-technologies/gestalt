package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/core"
)

func (s *Server) egressClientStore() (core.EgressClientStore, error) {
	ecs, ok := s.datastore.(core.EgressClientStore)
	if !ok {
		return nil, fmt.Errorf("datastore does not support egress clients; use a SQL-backed datastore (sqlite, postgres, mysql, sqlserver, oracle)")
	}
	return ecs, nil
}

func (s *Server) resolveOwnedEgressClient(w http.ResponseWriter, r *http.Request, ecs core.EgressClientStore) (*core.EgressClient, bool) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return nil, false
	}
	clientID := chi.URLParam(r, "id")
	client, err := ecs.GetEgressClient(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "egress client not found")
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "failed to look up egress client")
		return nil, false
	}
	if client.CreatedByID != userID {
		writeError(w, http.StatusNotFound, "egress client not found")
		return nil, false
	}
	return client, true
}

type createEgressClientRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type egressClientResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedByID string    `json:"created_by_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s *Server) createEgressClient(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	ecs, err := s.egressClientStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	var req createEgressClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	now := s.nowUTCSecond()
	client := &core.EgressClient{
		ID:          uuid.NewString(),
		Name:        req.Name,
		Description: req.Description,
		CreatedByID: userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := ecs.CreateEgressClient(r.Context(), client); err != nil {
		if errors.Is(err, core.ErrAlreadyRegistered) {
			writeError(w, http.StatusConflict, "egress client name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create egress client")
		return
	}

	writeJSON(w, http.StatusCreated, egressClientResponseFromCore(client))
}

func (s *Server) listEgressClients(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	ecs, err := s.egressClientStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	clients, err := ecs.ListEgressClients(r.Context(), core.EgressClientFilter{CreatedByID: userID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list egress clients")
		return
	}

	out := make([]egressClientResponse, 0, len(clients))
	for _, c := range clients {
		out = append(out, egressClientResponseFromCore(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteEgressClient(w http.ResponseWriter, r *http.Request) {
	ecs, err := s.egressClientStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	client, ok := s.resolveOwnedEgressClient(w, r, ecs)
	if !ok {
		return
	}

	if err := ecs.DeleteEgressClient(r.Context(), client.ID); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "egress client not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete egress client")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type createEgressClientTokenRequest struct {
	Name string `json:"name"`
}

type createEgressClientTokenResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Token     string     `json:"token"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type egressClientTokenInfo struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (s *Server) createEgressClientToken(w http.ResponseWriter, r *http.Request) {
	ecs, err := s.egressClientStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	client, ok := s.resolveOwnedEgressClient(w, r, ecs)
	if !ok {
		return
	}
	clientID := client.ID

	var req createEgressClientTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	issued, err := s.issueEgressClientToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	token := &core.EgressClientToken{
		ID:          uuid.NewString(),
		ClientID:    clientID,
		Name:        req.Name,
		HashedToken: issued.Hashed,
		ExpiresAt:   issued.ExpiresAt,
		CreatedAt:   issued.CreatedAt,
		UpdatedAt:   issued.UpdatedAt,
	}

	if err := ecs.CreateEgressClientToken(r.Context(), token); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create egress client token")
		return
	}

	writeJSON(w, http.StatusCreated, createEgressClientTokenResponse{
		ID:        token.ID,
		Name:      token.Name,
		Token:     issued.Plaintext,
		ExpiresAt: token.ExpiresAt,
	})
}

func (s *Server) listEgressClientTokens(w http.ResponseWriter, r *http.Request) {
	ecs, err := s.egressClientStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	client, ok := s.resolveOwnedEgressClient(w, r, ecs)
	if !ok {
		return
	}

	tokens, err := ecs.ListEgressClientTokens(r.Context(), client.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list egress client tokens")
		return
	}

	out := make([]egressClientTokenInfo, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, egressClientTokenInfoFromCore(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) revokeEgressClientToken(w http.ResponseWriter, r *http.Request) {
	ecs, err := s.egressClientStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	client, ok := s.resolveOwnedEgressClient(w, r, ecs)
	if !ok {
		return
	}

	clientID := client.ID
	tokenID := chi.URLParam(r, "tokenID")
	if err := ecs.RevokeEgressClientToken(r.Context(), clientID, tokenID); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke egress client token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
