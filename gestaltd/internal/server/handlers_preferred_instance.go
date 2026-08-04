package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
)

type selectPreferredInstanceRequest struct {
	Connection string `json:"connection"`
	Instance   string `json:"instance"`
}

type selectPreferredInstanceResponse struct {
	Status      string `json:"status"`
	Integration string `json:"integration"`
	Connection  string `json:"connection"`
	Instance    string `json:"instance"`
}

func (s *Server) selectPreferredInstance(w http.ResponseWriter, r *http.Request) {
	integration := chi.URLParam(r, "name")
	auditAllowed := false
	auditErr := errors.New("preferred instance selection failed")
	auditTarget := auditTarget{}
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), integration, "connection.select_instance", auditAllowed, auditErr, auditTarget)
	}()

	subjectID, err := s.resolveCredentialSubjectID(w, r, "")
	if err != nil {
		auditErr = err
		return
	}
	if _, ok := s.getProvider(r.Context(), w, integration); !ok {
		auditErr = errors.New("integration not found")
		return
	}

	var req selectPreferredInstanceRequest
	if err := decodeSelectPreferredInstanceRequest(r.Body, &req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	instance, ok := resolveRequestedInstance(w, req.Instance)
	if !ok {
		auditErr = errors.New("invalid instance")
		return
	}
	connection, ok := s.resolveRequestedConnection(w, integration, req.Connection)
	if !ok {
		auditErr = errors.New("invalid connection")
		return
	}
	auditTarget = connectionAuditTarget(integration, connection, instance)

	connDef, ok := s.effectiveConnectionDef(integration, connection)
	if !ok {
		auditErr = errors.New("connection not configured")
		writeError(w, http.StatusBadRequest, fmt.Sprintf("connection %q is not configured for integration %q", connection, integration))
		return
	}
	connectionID := serverCredentialConnectionID(integration, connection, connDef)
	if _, err := s.externalCredentials.GetCredential(r.Context(), subjectID, connectionID, instance); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			auditErr = errors.New("connection instance not found")
			writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q instance %q", integration, instance))
			return
		}
		auditErr = errors.New("failed to look up connection instance")
		writeError(w, http.StatusInternalServerError, "failed to look up connection instance")
		return
	}
	if s.connectionInstancePreferences == nil {
		auditErr = errors.New("preferred instance store is not configured")
		writeError(w, http.StatusInternalServerError, "preferred instance store is not configured")
		return
	}
	if _, err := s.connectionInstancePreferences.Set(r.Context(), subjectID, connectionID, instance); err != nil {
		auditErr = errors.New("failed to store preferred instance")
		writeError(w, http.StatusInternalServerError, "failed to store preferred instance")
		return
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, selectPreferredInstanceResponse{
		Status:      "selected",
		Integration: integration,
		Connection:  userFacingConnectionName(connection),
		Instance:    instance,
	})
}

func decodeSelectPreferredInstanceRequest(r io.Reader, req *selectPreferredInstanceRequest) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	return decoder.Decode(req)
}

func (s *Server) preferredInstanceForConnection(ctx context.Context, subjectID, connectionID string) string {
	if s == nil || s.connectionInstancePreferences == nil {
		return ""
	}
	subjectID = strings.TrimSpace(subjectID)
	connectionID = strings.TrimSpace(connectionID)
	if subjectID == "" || connectionID == "" {
		return ""
	}
	pref, err := s.connectionInstancePreferences.Get(ctx, subjectID, connectionID)
	if err != nil || pref == nil {
		return ""
	}
	return pref.Instance
}

func (s *Server) maybeSetDefaultInstancePreference(ctx context.Context, subjectID, integration, connection, instance string) {
	if s == nil || s.connectionInstancePreferences == nil {
		return
	}
	subjectID = strings.TrimSpace(subjectID)
	instance = strings.TrimSpace(instance)
	if subjectID == "" || instance == "" {
		return
	}
	connDef, ok := s.effectiveConnectionDef(integration, connection)
	if !ok {
		return
	}
	connectionID := serverCredentialConnectionID(integration, connection, connDef)
	if pref, err := s.connectionInstancePreferences.Get(ctx, subjectID, connectionID); err == nil && pref != nil {
		return
	} else if err != nil && !errors.Is(err, core.ErrNotFound) {
		return
	}
	credentials, err := s.externalCredentials.ListCredentials(ctx, subjectID, connectionID)
	if err != nil || len(credentials) != 1 {
		return
	}
	_, _ = s.connectionInstancePreferences.Set(ctx, subjectID, connectionID, instance)
}
