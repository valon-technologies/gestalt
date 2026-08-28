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
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

var errManagedSubjectsUnavailable = errors.New("managed subjects are unavailable")

type createAuthorizationSubjectRequest struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

type authorizationSubjectResponse struct {
	SubjectID   string `json:"subjectId"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
}

type createAuthorizationSubjectTokenRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	ExpiresIn   *int64   `json:"expiresIn,omitempty"`
}

func (s *Server) createAuthorizationSubject(w http.ResponseWriter, r *http.Request) {
	if err := requireUserCaller(w, PrincipalFromContext(r.Context())); err != nil {
		return
	}
	if s.managedSubjects == nil {
		writeError(w, http.StatusNotFound, "managed subjects are unavailable")
		return
	}

	var req createAuthorizationSubjectRequest
	if err := decodeAuthorizationSubjectRequest(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	localID := strings.TrimSpace(req.ID)
	if !validManagedSubjectLocalID(localID) {
		writeError(w, http.StatusBadRequest, "id must be 1-128 characters of [a-zA-Z0-9._-]")
		return
	}
	subjectID := coredata.ManagedSubjectKindServiceAccount + ":" + localID

	p := PrincipalFromContext(r.Context())
	callerSubjectID, err := principal.ResolveAuthorizationSubjectID(r.Context(), s.credentialUserResolver(), p)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	created, err := s.managedSubjects.CreateManagedSubject(r.Context(), &core.ManagedSubject{
		SubjectID:          subjectID,
		DisplayName:        strings.TrimSpace(req.DisplayName),
		Description:        strings.TrimSpace(req.Description),
		CreatedBySubjectID: callerSubjectID,
	})
	if err != nil {
		if errors.Is(err, core.ErrAlreadyRegistered) {
			writeError(w, http.StatusConflict, "subject already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create subject")
		return
	}

	writeJSON(w, http.StatusCreated, authorizationSubjectResponse{
		SubjectID:   created.SubjectID,
		DisplayName: created.DisplayName,
		Description: created.Description,
	})
}

func (s *Server) createAuthorizationSubjectToken(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("service account token creation failed")
	auditTarget := auditTarget{}
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "grant.create", auditAllowed, auditErr, auditTarget)
	}()

	if err := requireUserCaller(w, PrincipalFromContext(r.Context())); err != nil {
		auditErr = err
		return
	}
	if s.auth == nil {
		auditErr = errors.New("auth is disabled")
		writeError(w, http.StatusNotFound, "auth is disabled")
		return
	}

	subjectID, err := canonicalServiceAccountCredentialSubjectID(chi.URLParam(r, "subjectId"))
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.authorizeServiceAccountTokenMint(r.Context(), PrincipalFromContext(r.Context()), subjectID); err != nil {
		auditErr = err
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := s.requireManagedServiceAccountSubject(r.Context(), subjectID); err != nil {
		auditErr = err
		switch {
		case errors.Is(err, core.ErrNotFound):
			writeError(w, http.StatusNotFound, "managed subject not found")
		case errors.Is(err, errManagedSubjectsUnavailable):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to resolve managed subject")
		}
		return
	}

	var req createAuthorizationSubjectTokenRequest
	if err := decodeAuthorizationSubjectTokenRequest(r.Body, &req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	auditTarget = apiTokenAuditTarget("", req.Name)

	scope, err := s.validateSubjectTokenPermissions(r.Context(), req)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	expiresIn, err := tokenExpiresIn(req.ExpiresIn)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := s.callerAuthContext(r.Context(), r)
	callerToken, err := s.callerBearerToken(r)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusUnauthorized, "caller bearer token required")
		return
	}
	tokenResp, err := s.auth.Token(ctx, &core.TokenRequest{
		GrantType:        core.GrantTypeTokenExchange,
		SubjectToken:     callerToken,
		SubjectTokenType: core.SubjectTokenTypeAccessToken,
		Scope:            scope,
		ClientID:         core.DefaultOAuthClientID,
		ExpiresIn:        expiresIn,
		Name:             strings.TrimSpace(req.Name),
		GrantSubject:     subjectID,
	})
	grantID := ""
	if tokenResp != nil {
		grantID = strings.TrimSpace(tokenResp.GrantID)
	}
	if err != nil || tokenResp == nil || strings.TrimSpace(tokenResp.AccessToken) == "" || grantID == "" {
		auditErr = errors.New("failed to issue grant token")
		writeError(w, http.StatusInternalServerError, "failed to issue grant token")
		return
	}
	auditTarget = apiTokenAuditTarget(grantID, req.Name)

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusCreated, createGrantResponse{
		ID:        grantID,
		Name:      strings.TrimSpace(req.Name),
		Token:     tokenResp.AccessToken,
		Scopes:    principal.ParseScopeString(tokenResp.Scope),
		ExpiresAt: tokenExpiresAt(s.now, tokenResp.ExpiresIn),
	})
}

func (s *Server) validateSubjectTokenPermissions(ctx context.Context, req createAuthorizationSubjectTokenRequest) (string, error) {
	if strings.TrimSpace(req.Name) == "" {
		return "", errors.New("name is required")
	}
	if len(req.Permissions) == 0 {
		return "", errors.New("permissions is required")
	}
	parts := make([]string, 0, len(req.Permissions))
	for _, permission := range req.Permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			return "", errors.New("permissions must not contain empty values")
		}
		parts = append(parts, permission)
	}
	scope := strings.Join(parts, " ")
	return s.validateCreateGrantRequest(ctx, createTokenRequest{
		Name:   req.Name,
		Scopes: scope,
	})
}

func decodeAuthorizationSubjectRequest(r io.Reader, req *createAuthorizationSubjectRequest) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	return decoder.Decode(req)
}

func decodeAuthorizationSubjectTokenRequest(r io.Reader, req *createAuthorizationSubjectTokenRequest) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	return decoder.Decode(req)
}

type setAuthorizationSubjectGrantRequest struct {
	Relation     string `json:"relation"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
}

func (s *Server) setAuthorizationSubjectGrant(w http.ResponseWriter, r *http.Request) {
	if err := requireUserCaller(w, PrincipalFromContext(r.Context())); err != nil {
		return
	}
	if s.authorization == nil {
		writeError(w, http.StatusNotFound, "authorization is disabled")
		return
	}

	targetSubjectID, err := canonicalServiceAccountCredentialSubjectID(chi.URLParam(r, "subjectId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.authorizeServiceAccountGrantManagement(r.Context(), PrincipalFromContext(r.Context()), targetSubjectID); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := s.requireManagedServiceAccountSubject(r.Context(), targetSubjectID); err != nil {
		switch {
		case errors.Is(err, core.ErrNotFound):
			writeError(w, http.StatusNotFound, "managed subject not found")
		case errors.Is(err, errManagedSubjectsUnavailable):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to resolve managed subject")
		}
		return
	}

	var req setAuthorizationSubjectGrantRequest
	if err := decodeAuthorizationSubjectGrantRequest(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	relation := strings.TrimSpace(req.Relation)
	resourceType := strings.TrimSpace(req.ResourceType)
	resourceID := strings.TrimSpace(req.ResourceID)
	if relation == "" || resourceType == "" || resourceID == "" {
		writeError(w, http.StatusBadRequest, "relation, resourceType, and resourceId are required")
		return
	}

	_, err = s.authorization.AddRelationship(r.Context(), &proto.AddRelationshipRequest{
		Relationship: &proto.Relationship{
			Tuple: &proto.RelationshipTuple{
				Relation: relation,
				Resource: &proto.Resource{
					Type: resourceType,
					Id:   resourceID,
				},
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_Subject{
						Subject: &proto.Subject{
							Type: "subject",
							Id:   targetSubjectID,
						},
					},
				},
			},
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set grant")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

func decodeAuthorizationSubjectGrantRequest(r io.Reader, req *setAuthorizationSubjectGrantRequest) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	return decoder.Decode(req)
}

func (s *Server) requireManagedServiceAccountSubject(ctx context.Context, subjectID string) error {
	if s == nil || s.managedSubjects == nil {
		return errManagedSubjectsUnavailable
	}
	_, err := s.managedSubjects.GetManagedSubject(ctx, subjectID)
	return err
}

func (s *Server) authorizeServiceAccountTokenMint(ctx context.Context, p *principal.Principal, serviceAccountSubjectID string) error {
	if s == nil || s.authorization == nil {
		return fmt.Errorf("authorization provider is required")
	}
	subjectID, err := principal.ResolveAuthorizationSubjectID(ctx, s.credentialUserResolver(), p)
	if err != nil {
		return fmt.Errorf("not authenticated")
	}
	for _, action := range []string{"manages", "admin"} {
		allowed, err := invocation.CheckSubjectAccess(ctx, s.authorization, invocation.SubjectAccessRequest(subjectID, action, &proto.Resource{
			Type: "service_account",
			Id:   serviceAccountSubjectID,
		}))
		if err != nil {
			return fmt.Errorf("service account token mint denied: %w", err)
		}
		if allowed {
			return nil
		}
	}
	return fmt.Errorf("service account token mint denied")
}

func (s *Server) authorizeServiceAccountGrantManagement(ctx context.Context, p *principal.Principal, serviceAccountSubjectID string) error {
	if err := s.authorizeServiceAccountTokenMint(ctx, p, serviceAccountSubjectID); err == nil {
		return nil
	}
	subjectID, err := principal.ResolveAuthorizationSubjectID(ctx, s.credentialUserResolver(), p)
	if err != nil {
		return fmt.Errorf("not authenticated")
	}
	allowed, err := invocation.CheckSubjectAccess(ctx, s.authorization, invocation.SubjectAccessRequest(subjectID, "admin", &proto.Resource{
		Type: "managedSubject",
		Id:   serviceAccountSubjectID,
	}))
	if err != nil {
		return fmt.Errorf("managed subject grant denied: %w", err)
	}
	if !allowed {
		return fmt.Errorf("managed subject grant denied")
	}
	return nil
}
