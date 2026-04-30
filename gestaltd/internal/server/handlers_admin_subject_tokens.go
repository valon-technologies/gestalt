package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
)

func (s *Server) mountAdminSubjectTokenRoutes(r chi.Router) {
	r.Get("/subjects/{subjectID}/tokens", s.listAdminSubjectAPITokens)
	r.Post("/subjects/{subjectID}/tokens", s.createAdminSubjectAPIToken)
	r.Delete("/subjects/{subjectID}/tokens", s.revokeAllAdminSubjectAPITokens)
	r.Delete("/subjects/{subjectID}/tokens/{id}", s.revokeAdminSubjectAPIToken)
}

func (s *Server) createAdminSubjectAPIToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("subject api token creation failed")
	auditTarget := auditTarget{}
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "subject_api_token.create", auditAllowed, auditErr, auditTarget)
	}()

	subjectID, ok := adminSubjectTokenSubjectID(w, r)
	if !ok {
		auditErr = errors.New("invalid subject id")
		return
	}

	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		auditTarget = apiTokenAuditTarget("", req.Name)
	}

	permissions, err := s.validateCreateAPITokenRequest(req)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	apiToken, plaintext, err := s.issueOwnedAPIToken(r.Context(), &core.APIToken{
		OwnerKind:           core.APITokenOwnerKindSubject,
		OwnerID:             subjectID,
		CredentialSubjectID: subjectID,
		Name:                strings.TrimSpace(req.Name),
		Scopes:              req.Scopes,
		Permissions:         cloneAccessPermissions(permissions),
	}, false)
	if err != nil {
		auditErr = errors.New("failed to generate token")
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	auditTarget = apiTokenAuditTarget(apiToken.ID, apiToken.Name)

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusCreated, createTokenResponse{
		ID:          apiToken.ID,
		Name:        apiToken.Name,
		Token:       plaintext,
		Permissions: cloneAccessPermissions(apiToken.Permissions),
		ExpiresAt:   apiToken.ExpiresAt,
	})
}

func (s *Server) listAdminSubjectAPITokens(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("subject api token list failed")
	defer func() {
		s.auditHTTPEvent(r.Context(), PrincipalFromContext(r.Context()), "", "subject_api_token.list", auditAllowed, auditErr)
	}()

	subjectID, ok := adminSubjectTokenSubjectID(w, r)
	if !ok {
		auditErr = errors.New("invalid subject id")
		return
	}

	tokens, err := s.apiTokens.ListAPITokensByOwner(r.Context(), core.APITokenOwnerKindSubject, subjectID)
	if err != nil {
		auditErr = errors.New("failed to list tokens")
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	out := make([]apiTokenInfo, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, apiTokenInfoFromCore(token))
	}
	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) revokeAdminSubjectAPIToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("subject api token revoke failed")
	id := chi.URLParam(r, "id")
	auditTarget := apiTokenAuditTarget(id, "")
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "subject_api_token.revoke", auditAllowed, auditErr, auditTarget)
	}()

	subjectID, ok := adminSubjectTokenSubjectID(w, r)
	if !ok {
		auditErr = errors.New("invalid subject id")
		return
	}

	if err := s.apiTokens.RevokeAPITokenByOwner(r.Context(), core.APITokenOwnerKindSubject, subjectID, id); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			auditErr = errors.New("token not found")
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		auditErr = errors.New("failed to revoke token")
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}
	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) revokeAllAdminSubjectAPITokens(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("subject api token revoke all failed")
	auditTarget := apiTokenCollectionAuditTarget()
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "subject_api_token.revoke_all", auditAllowed, auditErr, auditTarget)
	}()

	subjectID, ok := adminSubjectTokenSubjectID(w, r)
	if !ok {
		auditErr = errors.New("invalid subject id")
		return
	}

	count, err := s.apiTokens.RevokeAllAPITokensByOwner(r.Context(), core.APITokenOwnerKindSubject, subjectID)
	if err != nil {
		auditErr = errors.New("failed to revoke tokens")
		writeError(w, http.StatusInternalServerError, "failed to revoke tokens")
		return
	}
	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "count": count})
}

func adminSubjectTokenSubjectID(w http.ResponseWriter, r *http.Request) (string, bool) {
	subjectID, err := adminAuthorizationSubjectID(chi.URLParam(r, "subjectID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	kind, _, ok := core.ParseSubjectID(subjectID)
	if !ok || kind == core.APITokenOwnerKindUser {
		writeError(w, http.StatusBadRequest, "subjectID must be a non-user canonical subject ID")
		return "", false
	}
	return subjectID, true
}
