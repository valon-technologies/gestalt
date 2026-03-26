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

func (s *Server) egressDenyRuleStore() (core.EgressDenyRuleStore, error) {
	edrs, ok := s.datastore.(core.EgressDenyRuleStore)
	if !ok {
		return nil, fmt.Errorf("datastore does not support egress deny rules; use a SQL-backed datastore (sqlite, postgres, mysql, sqlserver, oracle)")
	}
	return edrs, nil
}

type createEgressDenyRuleRequest struct {
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
	Provider    string `json:"provider"`
	Operation   string `json:"operation"`
	Method      string `json:"method"`
	Host        string `json:"host"`
	PathPrefix  string `json:"path_prefix"`
	Description string `json:"description"`
}

type egressDenyRuleResponse struct {
	ID          string    `json:"id"`
	SubjectKind string    `json:"subject_kind,omitempty"`
	SubjectID   string    `json:"subject_id,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	Operation   string    `json:"operation,omitempty"`
	Method      string    `json:"method,omitempty"`
	Host        string    `json:"host,omitempty"`
	PathPrefix  string    `json:"path_prefix,omitempty"`
	CreatedByID string    `json:"created_by_id"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func egressDenyRuleResponseFromCore(r *core.EgressDenyRule) egressDenyRuleResponse {
	return egressDenyRuleResponse{
		ID:          r.ID,
		SubjectKind: r.SubjectKind,
		SubjectID:   r.SubjectID,
		Provider:    r.Provider,
		Operation:   r.Operation,
		Method:      r.Method,
		Host:        r.Host,
		PathPrefix:  r.PathPrefix,
		CreatedByID: r.CreatedByID,
		Description: r.Description,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func (s *Server) createEgressDenyRule(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	store, err := s.egressDenyRuleStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	var req createEgressDenyRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.SubjectKind == "" && req.SubjectID == "" && req.Provider == "" &&
		req.Operation == "" && req.Method == "" && req.Host == "" && req.PathPrefix == "" {
		writeError(w, http.StatusBadRequest, "at least one match criterion is required")
		return
	}

	now := s.nowUTCSecond()
	rule := &core.EgressDenyRule{
		ID:          uuid.NewString(),
		SubjectKind: req.SubjectKind,
		SubjectID:   req.SubjectID,
		Provider:    req.Provider,
		Operation:   req.Operation,
		Method:      req.Method,
		Host:        req.Host,
		PathPrefix:  req.PathPrefix,
		CreatedByID: userID,
		Description: req.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateEgressDenyRule(r.Context(), rule); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create egress deny rule")
		return
	}

	writeJSON(w, http.StatusCreated, egressDenyRuleResponseFromCore(rule))
}

func (s *Server) listEgressDenyRules(w http.ResponseWriter, r *http.Request) {
	store, err := s.egressDenyRuleStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	filter := core.EgressDenyRuleFilter{
		SubjectKind: r.URL.Query().Get("subject_kind"),
		Host:        r.URL.Query().Get("host"),
	}

	rules, err := store.ListEgressDenyRules(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list egress deny rules")
		return
	}

	out := make([]egressDenyRuleResponse, 0, len(rules))
	for _, rule := range rules {
		out = append(out, egressDenyRuleResponseFromCore(rule))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteEgressDenyRule(w http.ResponseWriter, r *http.Request) {
	store, err := s.egressDenyRuleStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}

	id := chi.URLParam(r, "id")
	if err := store.DeleteEgressDenyRule(r.Context(), id); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "egress deny rule not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete egress deny rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
