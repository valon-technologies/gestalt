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
	"github.com/valon-technologies/gestalt/internal/egress"
)

func (s *Server) egressCredentialGrantStore() (core.EgressCredentialGrantStore, error) {
	ecgs, ok := s.datastore.(core.EgressCredentialGrantStore)
	if !ok {
		return nil, fmt.Errorf("datastore does not support egress credential grants; use a SQL-backed datastore (sqlite, postgres, mysql, sqlserver, oracle)")
	}
	return ecgs, nil
}

type createEgressCredentialGrantRequest struct {
	Provider    string `json:"provider"`
	Instance    string `json:"instance"`
	SecretRef   string `json:"secret_ref"`
	AuthStyle   string `json:"auth_style"`
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
	Operation   string `json:"operation"`
	Method      string `json:"method"`
	Host        string `json:"host"`
	PathPrefix  string `json:"path_prefix"`
}

type egressCredentialGrantResponse struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider,omitempty"`
	Instance    string    `json:"instance,omitempty"`
	SecretRef   string    `json:"secret_ref,omitempty"`
	AuthStyle   string    `json:"auth_style,omitempty"`
	SubjectKind string    `json:"subject_kind,omitempty"`
	SubjectID   string    `json:"subject_id,omitempty"`
	Operation   string    `json:"operation,omitempty"`
	Method      string    `json:"method,omitempty"`
	Host        string    `json:"host,omitempty"`
	PathPrefix  string    `json:"path_prefix,omitempty"`
	CreatedByID string    `json:"created_by_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func egressCredentialGrantResponseFromCore(g *core.EgressCredentialGrant) egressCredentialGrantResponse {
	return egressCredentialGrantResponse{
		ID:          g.ID,
		Provider:    g.Provider,
		Instance:    g.Instance,
		SecretRef:   g.SecretRef,
		AuthStyle:   g.AuthStyle,
		SubjectKind: g.SubjectKind,
		SubjectID:   g.SubjectID,
		Operation:   g.Operation,
		Method:      g.Method,
		Host:        g.Host,
		PathPrefix:  g.PathPrefix,
		CreatedByID: g.CreatedByID,
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
	}
}

func (s *Server) createEgressCredentialGrant(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	store, err := s.egressCredentialGrantStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	var req createEgressCredentialGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := egress.ValidateCredentialGrant(egress.CredentialGrantValidationInput{
		SubjectKind: req.SubjectKind,
		SubjectID:   req.SubjectID,
		Provider:    req.Provider,
		Instance:    req.Instance,
		Operation:   req.Operation,
		Method:      req.Method,
		Host:        req.Host,
		PathPrefix:  req.PathPrefix,
		AuthStyle:   req.AuthStyle,
	}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := s.nowUTCSecond()
	grant := &core.EgressCredentialGrant{
		ID:          uuid.NewString(),
		Provider:    req.Provider,
		Instance:    req.Instance,
		SecretRef:   req.SecretRef,
		AuthStyle:   req.AuthStyle,
		SubjectKind: req.SubjectKind,
		SubjectID:   req.SubjectID,
		Operation:   req.Operation,
		Method:      req.Method,
		Host:        req.Host,
		PathPrefix:  req.PathPrefix,
		CreatedByID: userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateEgressCredentialGrant(r.Context(), grant); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create egress credential grant")
		return
	}

	writeJSON(w, http.StatusCreated, egressCredentialGrantResponseFromCore(grant))
}

func (s *Server) listEgressCredentialGrants(w http.ResponseWriter, r *http.Request) {
	store, err := s.egressCredentialGrantStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	filter := core.EgressCredentialGrantFilter{
		Provider: r.URL.Query().Get("provider"),
	}

	grants, err := store.ListEgressCredentialGrants(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list egress credential grants")
		return
	}

	out := make([]egressCredentialGrantResponse, 0, len(grants))
	for _, g := range grants {
		out = append(out, egressCredentialGrantResponseFromCore(g))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteEgressCredentialGrant(w http.ResponseWriter, r *http.Request) {
	store, err := s.egressCredentialGrantStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	id := chi.URLParam(r, "id")
	if err := store.DeleteEgressCredentialGrant(r.Context(), id); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "egress credential grant not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete egress credential grant")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
