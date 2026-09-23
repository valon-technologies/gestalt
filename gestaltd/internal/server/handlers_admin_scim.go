package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type adminSCIMCredential struct {
	ID string `json:"id"`
}

type adminSCIMProjection struct {
	Relation     string `json:"relation"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
}

type adminSCIMClient struct {
	ID                       string                `json:"id"`
	Credentials              []adminSCIMCredential `json:"credentials"`
	AuthoritativeUserDomains []string              `json:"authoritativeUserDomains"`
	ActiveUserRelationships  []adminSCIMProjection `json:"activeUserRelationships"`
	Enabled                  bool                  `json:"enabled"`
	Retained                 bool                  `json:"retained"`
	Revision                 int64                 `json:"revision"`
	CreatedAt                string                `json:"createdAt,omitempty"`
	UpdatedAt                string                `json:"updatedAt,omitempty"`
	UpdatedBy                string                `json:"updatedBy,omitempty"`
}

type adminSCIMListResponse struct {
	Clients []adminSCIMClient `json:"clients"`
	Source  string            `json:"source"`
}

type adminSCIMClientRequest struct {
	ID                       string                     `json:"id"`
	Credentials              []adminSCIMCredentialInput `json:"credentials"`
	AuthoritativeUserDomains []string                   `json:"authoritativeUserDomains"`
	ActiveUserRelationships  []adminSCIMProjectionInput `json:"activeUserRelationships"`
	Enabled                  *bool                      `json:"enabled"`
	Retained                 *bool                      `json:"retained"`
	Revision                 int64                      `json:"revision"`
	Reason                   string                     `json:"reason"`
}

type adminSCIMCredentialInput struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type adminSCIMProjectionInput struct {
	Relation     string `json:"relation"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
}

type adminSCIMClientResponse struct {
	Client adminSCIMClient `json:"client"`
	Source string          `json:"source"`
}

func (s *Server) mountAdminSCIMRoutes(r chi.Router) {
	r.Get("/scim/clients", s.listAdminSCIMClients)
	r.Post("/scim/clients", s.createAdminSCIMClient)
	r.Patch("/scim/clients/{client}", s.updateAdminSCIMClient)
	r.Delete("/scim/clients/{client}", s.deleteAdminSCIMClient)
}

func (s *Server) scimRuntimeAdmin() *SCIMRuntime { return s.scimAdminRuntime }

func (s *Server) listAdminSCIMClients(w http.ResponseWriter, r *http.Request) {
	runtime := s.scimRuntimeAdmin()
	if runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "SCIM configuration is unavailable")
		return
	}
	clients, source, err := runtime.Current(r.Context())
	if err != nil {
		writeAdminSCIMError(w, err)
		return
	}
	out := make([]adminSCIMClient, 0, len(clients))
	for _, client := range clients {
		out = append(out, adminSCIMClientFromCore(client))
	}
	writeJSON(w, http.StatusOK, adminSCIMListResponse{Clients: out, Source: source})
}

func (s *Server) createAdminSCIMClient(w http.ResponseWriter, r *http.Request) {
	runtime := s.scimRuntimeAdmin()
	if runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "SCIM configuration is unavailable")
		return
	}
	var request adminSCIMClientRequest
	if err := decodeAdminSCIMRequest(w, r, &request); err != nil {
		return
	}
	record, err := adminSCIMRecordFromRequest(request, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	actor, ok := s.adminSCIMActor(w, r)
	if !ok {
		return
	}
	saved, err := runtime.Put(r.Context(), record, actor, false, 0)
	if err != nil {
		writeAdminSCIMError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, adminSCIMClientResponse{Client: adminSCIMClientFromCore(saved), Source: "runtime"})
}

func (s *Server) updateAdminSCIMClient(w http.ResponseWriter, r *http.Request) {
	runtime := s.scimRuntimeAdmin()
	if runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "SCIM configuration is unavailable")
		return
	}
	clientID := strings.TrimSpace(chi.URLParam(r, "client"))
	var request adminSCIMClientRequest
	if err := decodeAdminSCIMRequest(w, r, &request); err != nil {
		return
	}
	existing, _, err := runtime.Current(r.Context())
	if err != nil {
		writeAdminSCIMError(w, err)
		return
	}
	var prior *coredata.SCIMConfigRecord
	for _, client := range existing {
		if client.ID == clientID {
			prior = client
			break
		}
	}
	if prior == nil {
		writeError(w, http.StatusNotFound, "SCIM client not found")
		return
	}
	record, err := adminSCIMRecordFromRequest(request, prior)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	record.ID = clientID
	actor, ok := s.adminSCIMActor(w, r)
	if !ok {
		return
	}
	revision := request.Revision
	saved, err := runtime.Put(r.Context(), record, actor, revision > 0, revision)
	if err != nil {
		writeAdminSCIMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, adminSCIMClientResponse{Client: adminSCIMClientFromCore(saved), Source: "runtime"})
}

func (s *Server) deleteAdminSCIMClient(w http.ResponseWriter, r *http.Request) {
	runtime := s.scimRuntimeAdmin()
	if runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "SCIM configuration is unavailable")
		return
	}
	clientID := strings.TrimSpace(chi.URLParam(r, "client"))
	actor, ok := s.adminSCIMActor(w, r)
	if !ok {
		return
	}
	saved, err := runtime.Delete(r.Context(), clientID, actor)
	if err != nil {
		writeAdminSCIMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, adminSCIMClientResponse{Client: adminSCIMClientFromCore(saved), Source: "runtime"})
}

func adminSCIMClientFromCore(client *coredata.SCIMConfigRecord) adminSCIMClient {
	if client == nil {
		return adminSCIMClient{}
	}
	out := adminSCIMClient{
		ID:                       client.ID,
		Credentials:              make([]adminSCIMCredential, 0, len(client.Credentials)),
		AuthoritativeUserDomains: client.AuthoritativeUserDomains,
		ActiveUserRelationships:  make([]adminSCIMProjection, 0, len(client.ActiveUserRelationships)),
		Enabled:                  client.Enabled,
		Retained:                 client.Retained,
		Revision:                 client.Revision,
		CreatedAt:                formatAdminTime(client.CreatedAt),
		UpdatedAt:                formatAdminTime(client.UpdatedAt),
		UpdatedBy:                client.UpdatedBy,
	}
	for _, credential := range client.Credentials {
		out.Credentials = append(out.Credentials, adminSCIMCredential{ID: credential.ID})
	}
	for _, projection := range client.ActiveUserRelationships {
		out.ActiveUserRelationships = append(out.ActiveUserRelationships, adminSCIMProjection{
			Relation: projection.Relation, ResourceType: projection.ResourceType, ResourceID: projection.ResourceID,
		})
	}
	return out
}

func adminSCIMRecordFromRequest(request adminSCIMClientRequest, prior *coredata.SCIMConfigRecord) (*coredata.SCIMConfigRecord, error) {
	id := strings.TrimSpace(request.ID)
	if id == "" && prior != nil {
		id = prior.ID
	}
	if id == "" || id != strings.TrimSpace(id) {
		return nil, fmt.Errorf("id must be non-empty and trimmed")
	}
	if len(request.Credentials) < 1 || len(request.Credentials) > 2 {
		return nil, fmt.Errorf("credentials must contain one or two entries")
	}
	record := &coredata.SCIMConfigRecord{
		ID:       id,
		Enabled:  request.Enabled == nil || *request.Enabled,
		Retained: request.Retained != nil && *request.Retained,
	}
	if prior != nil {
		record.CreatedAt = prior.CreatedAt
		record.Revision = prior.Revision
	}
	seenIDs := map[string]struct{}{}
	seenTokens := map[string]struct{}{}
	priorTokens := map[string]string{}
	if prior != nil {
		for _, credential := range prior.Credentials {
			priorTokens[credential.ID] = credential.TokenRef
		}
	}
	for _, credential := range request.Credentials {
		credentialID := strings.TrimSpace(credential.ID)
		if credentialID == "" {
			return nil, fmt.Errorf("credential id is required")
		}
		if _, duplicate := seenIDs[credentialID]; duplicate {
			return nil, fmt.Errorf("credential id %q duplicates another credential", credentialID)
		}
		seenIDs[credentialID] = struct{}{}
		tokenRef := priorTokens[credentialID]
		token := strings.TrimSpace(credential.Token)
		if token != "" {
			if _, duplicate := seenTokens[token]; duplicate {
				return nil, fmt.Errorf("credential token duplicates another credential")
			}
			seenTokens[token] = struct{}{}
			tokenRef = token
		} else if tokenRef == "" {
			return nil, fmt.Errorf("credential token is required")
		}
		record.Credentials = append(record.Credentials, coredata.SCIMCredentialRecord{ID: credentialID, TokenRef: tokenRef})
	}
	for _, rawDomain := range request.AuthoritativeUserDomains {
		domain := strings.ToLower(strings.TrimSpace(rawDomain))
		if domain == "" || domain != rawDomain || strings.Contains(domain, "@") {
			return nil, fmt.Errorf("authoritativeUserDomains must be normalized domains")
		}
		record.AuthoritativeUserDomains = append(record.AuthoritativeUserDomains, domain)
	}
	seenProjections := map[string]struct{}{}
	for _, projection := range request.ActiveUserRelationships {
		relation := strings.TrimSpace(projection.Relation)
		resourceType := strings.TrimSpace(projection.ResourceType)
		resourceID := strings.TrimSpace(projection.ResourceID)
		if relation == "" || resourceType == "" || resourceID == "" {
			return nil, fmt.Errorf("activeUserRelationships relation, resourceType, and resourceId are required")
		}
		key := resourceType + "\x00" + resourceID + "\x00" + relation
		if _, duplicate := seenProjections[key]; duplicate {
			return nil, fmt.Errorf("activeUserRelationships contains a duplicate projection")
		}
		seenProjections[key] = struct{}{}
		record.ActiveUserRelationships = append(record.ActiveUserRelationships, coredata.SCIMRelationshipRecord{
			Relation: relation, ResourceType: resourceType, ResourceID: resourceID,
		})
	}
	return record, nil
}

func (s *Server) adminSCIMActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	if p := PrincipalFromContext(r.Context()); p != nil && strings.TrimSpace(p.SubjectID) != "" {
		return strings.TrimSpace(p.SubjectID), true
	}
	return "admin-api", true
}

func decodeAdminSCIMRequest(w http.ResponseWriter, r *http.Request, target any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return err
	}
	return nil
}

func writeAdminSCIMError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, coredata.ErrSCIMConfigNotFound):
		writeError(w, http.StatusNotFound, "SCIM client not found")
	case errors.Is(err, coredata.ErrSCIMConfigConflict):
		writeError(w, http.StatusConflict, "SCIM client was modified concurrently; reload and retry")
	case errors.Is(err, coredata.ErrSCIMClientExists):
		writeError(w, http.StatusConflict, "SCIM client was modified concurrently; reload and retry")
	case errors.Is(err, ErrSCIMRuntimeUnavailable):
		writeError(w, http.StatusServiceUnavailable, "SCIM configuration is unavailable")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
