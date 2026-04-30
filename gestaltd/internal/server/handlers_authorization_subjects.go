package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type createManagedSubjectRequest struct {
	ID          string `json:"id"`
	SubjectID   string `json:"subjectId"`
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

type updateManagedSubjectRequest struct {
	DisplayName *string `json:"displayName"`
	Description *string `json:"description"`
}

type managedSubjectInfo struct {
	ID                  string     `json:"id"`
	SubjectID           string     `json:"subjectId"`
	Kind                string     `json:"kind"`
	DisplayName         string     `json:"displayName"`
	Description         string     `json:"description,omitempty"`
	CredentialSubjectID string     `json:"credentialSubjectId"`
	CreatedBySubjectID  string     `json:"createdBySubjectId,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	DeletedAt           *time.Time `json:"deletedAt,omitempty"`
}

type managedSubjectMemberInfo struct {
	SubjectID string `json:"subjectId"`
	Role      string `json:"role"`
	Email     string `json:"email,omitempty"`
}

type managedSubjectGrantInfo struct {
	Plugin  string `json:"plugin"`
	Role    string `json:"role"`
	Source  string `json:"source"`
	Mutable bool   `json:"mutable"`
}

func (s *Server) mountAuthorizationSubjectRoutes(r chi.Router) {
	r.Get("/authorization/subjects", s.listManagedSubjects)
	r.Post("/authorization/subjects", s.createManagedSubject)
	r.Get("/authorization/subjects/{subjectID}", s.getManagedSubject)
	r.Patch("/authorization/subjects/{subjectID}", s.updateManagedSubject)
	r.Delete("/authorization/subjects/{subjectID}", s.deleteManagedSubject)

	r.Get("/authorization/subjects/{subjectID}/members", s.listManagedSubjectMembers)
	r.Put("/authorization/subjects/{subjectID}/members", s.putManagedSubjectMember)
	r.Delete("/authorization/subjects/{subjectID}/members/{memberSubjectID}", s.deleteManagedSubjectMember)

	r.Get("/authorization/subjects/{subjectID}/grants", s.listManagedSubjectGrants)
	r.Put("/authorization/subjects/{subjectID}/grants/{plugin}", s.putManagedSubjectGrant)
	r.Delete("/authorization/subjects/{subjectID}/grants/{plugin}", s.deleteManagedSubjectGrant)

	r.Get("/authorization/subjects/{subjectID}/tokens", s.listManagedSubjectAPITokens)
	r.Post("/authorization/subjects/{subjectID}/tokens", s.createManagedSubjectAPIToken)
	r.Delete("/authorization/subjects/{subjectID}/tokens", s.revokeAllManagedSubjectAPITokens)
	r.Delete("/authorization/subjects/{subjectID}/tokens/{id}", s.revokeManagedSubjectAPIToken)
}

func (s *Server) createManagedSubject(w http.ResponseWriter, r *http.Request) {
	creatorSubjectID, ok := s.currentManagedSubjectUserSubjectID(w, r)
	if !ok {
		return
	}
	if !s.ensureManagedSubjectsAvailable(w) {
		return
	}

	var req createManagedSubjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	subjectID, err := managedSubjectIDFromCreateRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		_, id, _ := core.ParseSubjectID(subjectID)
		displayName = id
	}

	subject, err := s.managedSubjects.CreateManagedSubject(r.Context(), &core.ManagedSubject{
		SubjectID:           subjectID,
		Kind:                coredata.ManagedSubjectKindServiceAccount,
		DisplayName:         displayName,
		Description:         req.Description,
		CredentialSubjectID: subjectID,
		CreatedBySubjectID:  creatorSubjectID,
	})
	if err != nil {
		if errors.Is(err, core.ErrAlreadyRegistered) {
			writeError(w, http.StatusConflict, "managed subject already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create managed subject")
		return
	}

	if err := s.writeManagedSubjectMembership(r.Context(), subject.SubjectID, creatorSubjectID, authorization.ProviderManagedSubjectRelationAdmin); err != nil {
		if rollbackErr := s.managedSubjects.RemoveManagedSubjectForRollback(r.Context(), subject.SubjectID); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist managed subject owner and rollback metadata")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to persist managed subject owner")
		return
	}

	writeJSON(w, http.StatusCreated, managedSubjectInfoFromCore(subject))
}

func (s *Server) listManagedSubjects(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentManagedSubjectUserSubjectID(w, r); !ok {
		return
	}
	if !s.ensureManagedSubjectsAvailable(w) {
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = coredata.ManagedSubjectKindServiceAccount
	}
	subjects, err := s.managedSubjects.ListManagedSubjects(r.Context(), kind)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]managedSubjectInfo, 0, len(subjects))
	for _, subject := range subjects {
		if subject == nil {
			continue
		}
		allowed, err := s.managedSubjectActionAllowed(r.Context(), subject.SubjectID, authorization.ProviderManagedSubjectActionView)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to evaluate managed subject access")
			return
		}
		if !allowed {
			continue
		}
		out = append(out, managedSubjectInfoFromCore(subject))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayName != out[j].DisplayName {
			return out[i].DisplayName < out[j].DisplayName
		}
		return out[i].SubjectID < out[j].SubjectID
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getManagedSubject(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionView) {
		return
	}
	writeJSON(w, http.StatusOK, managedSubjectInfoFromCore(subject))
}

func (s *Server) updateManagedSubject(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionManage) {
		return
	}

	var req updateManagedSubjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	updated, err := s.managedSubjects.UpdateManagedSubject(r.Context(), subject.SubjectID, func(subject *core.ManagedSubject) error {
		if req.DisplayName != nil {
			subject.DisplayName = strings.TrimSpace(*req.DisplayName)
		}
		if req.Description != nil {
			subject.Description = strings.TrimSpace(*req.Description)
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update managed subject")
		return
	}
	writeJSON(w, http.StatusOK, managedSubjectInfoFromCore(updated))
}

func (s *Server) deleteManagedSubject(w http.ResponseWriter, r *http.Request) {
	subjectID, ok := s.managedSubjectIDFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subjectID, authorization.ProviderManagedSubjectActionManage) {
		return
	}

	if _, err := s.apiTokens.RevokeAllAPITokensByOwner(r.Context(), core.APITokenOwnerKindSubject, subjectID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke managed subject tokens")
		return
	}
	if err := s.deleteManagedSubjectCredentials(r.Context(), subjectID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete managed subject credentials")
		return
	}
	if err := s.deleteManagedSubjectSubjectRelationships(r.Context(), subjectID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete managed subject authorization relationships")
		return
	}
	if _, err := s.managedSubjects.DeleteManagedSubject(r.Context(), subjectID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete managed subject")
		return
	}
	if err := s.deleteManagedSubjectResourceRelationships(r.Context(), subjectID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete managed subject authorization relationships")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) listManagedSubjectMembers(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionView) {
		return
	}
	members, err := s.managedSubjectMemberRows(r.Context(), subject.SubjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list managed subject members")
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) putManagedSubjectMember(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionManage) {
		return
	}
	var req putAdminAuthorizationMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !managedSubjectManagementRole(req.Role) {
		writeError(w, http.StatusBadRequest, "role must be viewer, editor, or admin")
		return
	}
	member, status, message := s.resolveAdminAuthorizationWriteSubject(r.Context(), req)
	if status != 0 {
		writeError(w, status, message)
		return
	}
	if err := s.writeManagedSubjectMembership(r.Context(), subject.SubjectID, member.SubjectID, strings.TrimSpace(req.Role)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist managed subject member")
		return
	}
	writeJSON(w, http.StatusOK, managedSubjectMemberInfo{
		SubjectID: member.SubjectID,
		Role:      strings.TrimSpace(req.Role),
		Email:     adminAuthorizationSubjectEmail(member),
	})
}

func (s *Server) deleteManagedSubjectMember(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionManage) {
		return
	}
	memberSubjectID, err := adminAuthorizationSubjectID(strings.TrimSpace(chi.URLParam(r, "memberSubjectID")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resource := managedSubjectResource(subject.SubjectID)
	existing, _, err := s.deleteProviderDynamicMembership(r.Context(), resource, memberSubjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete managed subject member")
		return
	}
	if len(existing) == 0 {
		writeError(w, http.StatusNotFound, "managed subject member not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) listManagedSubjectGrants(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionView) {
		return
	}
	grants, err := s.managedSubjectGrantRows(r.Context(), subject.SubjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list managed subject grants")
		return
	}
	writeJSON(w, http.StatusOK, grants)
}

func (s *Server) putManagedSubjectGrant(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionGrant) {
		return
	}
	plugin, _, err := s.adminAuthorizationPluginEntry(chi.URLParam(r, "plugin"))
	if err != nil {
		s.writeAdminAuthorizationPluginError(w, err)
		return
	}
	if access, ok := s.authorizer.StaticRoleForProviderIdentity(plugin, subject.SubjectID); ok && access.Role != "" {
		writeError(w, http.StatusConflict, "subject already has static authorization for this plugin")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Role) == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}
	if !s.requireManagedSubjectPluginDelegation(w, r, plugin, req.Role) {
		return
	}
	membership, err := s.upsertProviderPluginAuthorization(r.Context(), &adminAuthorizationWriteSubject{SubjectID: subject.SubjectID}, plugin, req.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist managed subject grant")
		return
	}
	if err := s.reloadAuthorizationState(r.Context()); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":   "persisted_pending_reload",
			"grant":    managedSubjectGrantInfo{Plugin: plugin, Role: membership.Role, Source: "dynamic", Mutable: true},
			"reloaded": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, managedSubjectGrantInfo{Plugin: plugin, Role: membership.Role, Source: "dynamic", Mutable: true})
}

func (s *Server) deleteManagedSubjectGrant(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionGrant) {
		return
	}
	plugin, _, err := s.adminAuthorizationPluginEntry(chi.URLParam(r, "plugin"))
	if err != nil {
		s.writeAdminAuthorizationPluginError(w, err)
		return
	}
	if err := s.deleteProviderPluginAuthorization(r.Context(), plugin, subject.SubjectID); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "managed subject grant not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete managed subject grant")
		return
	}
	if err := s.reloadAuthorizationState(r.Context()); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "deleted_pending_reload", "reloaded": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) createManagedSubjectAPIToken(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionCreateToken) {
		return
	}
	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	permissions, err := s.validateCreateAPITokenRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	apiToken, plaintext, err := s.issueOwnedAPIToken(r.Context(), &core.APIToken{
		OwnerKind:           core.APITokenOwnerKindSubject,
		OwnerID:             subject.SubjectID,
		CredentialSubjectID: subject.SubjectID,
		Name:                strings.TrimSpace(req.Name),
		Scopes:              req.Scopes,
		Permissions:         cloneAccessPermissions(permissions),
	}, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	writeJSON(w, http.StatusCreated, createTokenResponse{
		ID:          apiToken.ID,
		Name:        apiToken.Name,
		Token:       plaintext,
		Permissions: cloneAccessPermissions(apiToken.Permissions),
		ExpiresAt:   apiToken.ExpiresAt,
	})
}

func (s *Server) listManagedSubjectAPITokens(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionView) {
		return
	}
	tokens, err := s.apiTokens.ListAPITokensByOwner(r.Context(), core.APITokenOwnerKindSubject, subject.SubjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list managed subject tokens")
		return
	}
	out := make([]apiTokenInfo, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, apiTokenInfoFromCore(token))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) revokeManagedSubjectAPIToken(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionManage) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := s.apiTokens.RevokeAPITokenByOwner(r.Context(), core.APITokenOwnerKindSubject, subject.SubjectID, id); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) revokeAllManagedSubjectAPITokens(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.managedSubjectFromPath(w, r)
	if !ok {
		return
	}
	if !s.requireManagedSubjectAction(w, r, subject.SubjectID, authorization.ProviderManagedSubjectActionManage) {
		return
	}
	count, err := s.apiTokens.RevokeAllAPITokensByOwner(r.Context(), core.APITokenOwnerKindSubject, subject.SubjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke managed subject tokens")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "count": count})
}

func (s *Server) currentUserSubjectID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, err := s.resolveUserID(w, r)
	if err != nil {
		return "", false
	}
	return principal.UserSubjectID(userID), true
}

func (s *Server) currentManagedSubjectUserSubjectID(w http.ResponseWriter, r *http.Request) (string, bool) {
	subjectID, ok := s.currentUserSubjectID(w, r)
	if !ok {
		return "", false
	}
	if !s.requireUnscopedManagedSubjectCaller(w, r) {
		return "", false
	}
	return subjectID, true
}

func (s *Server) requireUnscopedManagedSubjectCaller(w http.ResponseWriter, r *http.Request) bool {
	p := principal.Canonicalized(PrincipalFromContext(r.Context()))
	if p == nil || p.Source != principal.SourceAPIToken {
		return true
	}
	if p.TokenPermissions == nil && p.ActionPermissions == nil && len(p.Scopes) == 0 {
		return true
	}
	writeError(w, http.StatusForbidden, "scoped API tokens cannot manage managed subjects")
	return false
}

func (s *Server) ensureManagedSubjectsAvailable(w http.ResponseWriter) bool {
	if s.managedSubjects == nil {
		writeError(w, http.StatusServiceUnavailable, "managed subjects are unavailable")
		return false
	}
	if s.authorizationProvider == nil || s.authorizer == nil {
		writeError(w, http.StatusServiceUnavailable, "managed subjects require an authorization provider")
		return false
	}
	return true
}

func (s *Server) managedSubjectFromPath(w http.ResponseWriter, r *http.Request) (*core.ManagedSubject, bool) {
	subjectID, ok := s.managedSubjectIDFromPath(w, r)
	if !ok {
		return nil, false
	}
	subject, err := s.managedSubjects.GetManagedSubject(r.Context(), subjectID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "managed subject not found")
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load managed subject")
		return nil, false
	}
	return subject, true
}

func (s *Server) managedSubjectIDFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !s.ensureManagedSubjectsAvailable(w) {
		return "", false
	}
	subjectID, err := canonicalServiceAccountSubjectID(chi.URLParam(r, "subjectID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return subjectID, true
}

func (s *Server) requireManagedSubjectAction(w http.ResponseWriter, r *http.Request, subjectID, action string) bool {
	if _, ok := s.currentManagedSubjectUserSubjectID(w, r); !ok {
		return false
	}
	allowed, err := s.managedSubjectActionAllowed(r.Context(), subjectID, action)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to evaluate managed subject access")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "managed subject access denied")
		return false
	}
	return true
}

func (s *Server) managedSubjectActionAllowed(ctx context.Context, subjectID, action string) (bool, error) {
	p := PrincipalFromContext(ctx)
	p = principal.Canonicalized(p)
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		return false, nil
	}
	if s.authorizationProvider == nil {
		return false, fmt.Errorf("authorization provider is unavailable")
	}
	roles := managedSubjectActionRoles(action)
	reqs := make([]*core.AccessEvaluationRequest, 0, len(roles))
	for _, role := range roles {
		reqs = append(reqs, &core.AccessEvaluationRequest{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: strings.TrimSpace(p.SubjectID)},
			Action:   &core.ActionRef{Name: role},
			Resource: managedSubjectResource(subjectID),
		})
	}
	resp, err := s.authorizationProvider.EvaluateMany(ctx, &core.AccessEvaluationsRequest{Requests: reqs})
	if err != nil {
		return false, err
	}
	for _, decision := range resp.GetDecisions() {
		if decision.GetAllowed() {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) writeManagedSubjectMembership(ctx context.Context, subjectID, memberSubjectID, role string) error {
	if !managedSubjectManagementRole(role) {
		return fmt.Errorf("unsupported managed subject role %q", role)
	}
	_, _, err := s.replaceProviderDynamicMembership(ctx, managedSubjectResource(subjectID), memberSubjectID, role)
	return err
}

func (s *Server) requireManagedSubjectPluginDelegation(w http.ResponseWriter, r *http.Request, plugin, role string) bool {
	p := principal.Canonicalized(PrincipalFromContext(r.Context()))
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		writeError(w, http.StatusForbidden, "plugin access denied")
		return false
	}
	access, allowed := s.authorizer.ResolveAccess(r.Context(), p, plugin)
	if !allowed {
		writeError(w, http.StatusForbidden, "plugin access denied")
		return false
	}
	requestedRole := strings.TrimSpace(role)
	callerRole := strings.TrimSpace(access.Role)
	if access.Policy == "" || managedSubjectPluginRoleCanDelegate(callerRole, requestedRole) {
		return true
	}
	writeError(w, http.StatusForbidden, "plugin role cannot grant requested role")
	return false
}

func (s *Server) managedSubjectMemberRows(ctx context.Context, subjectID string) ([]managedSubjectMemberInfo, error) {
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Resource: managedSubjectResource(subjectID),
	})
	if err != nil {
		return nil, err
	}
	out := make([]managedSubjectMemberInfo, 0, len(relationships))
	for _, rel := range relationships {
		if rel == nil || rel.GetSubject() == nil || !managedSubjectManagementRole(rel.GetRelation()) {
			continue
		}
		if rel.GetSubject().GetType() != authorization.ProviderSubjectTypeSubject {
			continue
		}
		row := managedSubjectMemberInfo{
			SubjectID: rel.GetSubject().GetId(),
			Role:      rel.GetRelation(),
		}
		if userID := principal.UserIDFromSubjectID(row.SubjectID); userID != "" {
			if user, err := s.users.GetUser(ctx, userID); err == nil && user != nil {
				row.Email = user.Email
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return managedSubjectRoleSortKey(out[i].Role) < managedSubjectRoleSortKey(out[j].Role)
		}
		return out[i].SubjectID < out[j].SubjectID
	})
	return out, nil
}

func (s *Server) managedSubjectGrantRows(ctx context.Context, subjectID string) ([]managedSubjectGrantInfo, error) {
	byPlugin := map[string]managedSubjectGrantInfo{}
	for plugin := range s.pluginDefs {
		if access, ok := s.authorizer.StaticRoleForProviderIdentity(plugin, subjectID); ok && access.Role != "" {
			byPlugin[plugin] = managedSubjectGrantInfo{Plugin: plugin, Role: access.Role, Source: "static", Mutable: false}
		}
	}
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: subjectID},
	})
	if err != nil {
		return nil, err
	}
	for _, rel := range relationships {
		if rel == nil || rel.GetResource() == nil || rel.GetResource().GetType() != authorization.ProviderResourceTypePluginDynamic {
			continue
		}
		plugin := strings.TrimSpace(rel.GetResource().GetId())
		if plugin == "" {
			continue
		}
		if existing, ok := byPlugin[plugin]; ok && existing.Source == "static" {
			continue
		}
		byPlugin[plugin] = managedSubjectGrantInfo{Plugin: plugin, Role: strings.TrimSpace(rel.GetRelation()), Source: "dynamic", Mutable: true}
	}
	out := make([]managedSubjectGrantInfo, 0, len(byPlugin))
	for _, grant := range byPlugin {
		out = append(out, grant)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Plugin < out[j].Plugin })
	return out, nil
}

func (s *Server) deleteManagedSubjectCredentials(ctx context.Context, subjectID string) error {
	if s.externalCredentials == nil || coredata.ExternalCredentialProviderMissing(s.externalCredentials) {
		return nil
	}
	credentials, err := s.externalCredentials.ListCredentials(ctx, subjectID)
	if err != nil {
		return err
	}
	for _, credential := range credentials {
		if credential == nil || strings.TrimSpace(credential.ID) == "" {
			continue
		}
		if err := s.externalCredentials.DeleteCredential(ctx, credential.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) deleteManagedSubjectSubjectRelationships(ctx context.Context, subjectID string) error {
	if s.authorizationProvider == nil {
		return errAdminAuthorizationUnavailable
	}
	subjectID = strings.TrimSpace(subjectID)
	subjectRels, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: subjectID},
	})
	if err != nil {
		return err
	}
	return s.deleteAuthorizationRelationships(ctx, subjectRels)
}

func (s *Server) deleteManagedSubjectResourceRelationships(ctx context.Context, subjectID string) error {
	if s.authorizationProvider == nil {
		return errAdminAuthorizationUnavailable
	}
	resourceRels, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Resource: managedSubjectResource(subjectID),
	})
	if err != nil {
		return err
	}
	return s.deleteAuthorizationRelationships(ctx, resourceRels)
}

func (s *Server) deleteAuthorizationRelationships(ctx context.Context, relationships []*core.Relationship) error {
	relationshipsByKey := map[string]*core.Relationship{}
	for _, rel := range relationships {
		if rel == nil {
			continue
		}
		relationshipsByKey[providerRelationshipKey(rel)] = rel
	}
	if len(relationshipsByKey) == 0 {
		return nil
	}
	rels := make([]*core.Relationship, 0, len(relationshipsByKey))
	for _, rel := range relationshipsByKey {
		rels = append(rels, rel)
	}
	modelID, err := s.managedAuthorizationModelID(ctx)
	if err != nil {
		return err
	}
	return s.authorizationProvider.WriteRelationships(ctx, &core.WriteRelationshipsRequest{
		Deletes: relationshipKeys(rels),
		ModelId: modelID,
	})
}

func managedSubjectResource(subjectID string) *core.ResourceRef {
	return &core.ResourceRef{Type: authorization.ProviderResourceTypeManagedSubject, Id: strings.TrimSpace(subjectID)}
}

func managedSubjectInfoFromCore(subject *core.ManagedSubject) managedSubjectInfo {
	if subject == nil {
		return managedSubjectInfo{}
	}
	_, id, _ := core.ParseSubjectID(subject.SubjectID)
	return managedSubjectInfo{
		ID:                  id,
		SubjectID:           subject.SubjectID,
		Kind:                subject.Kind,
		DisplayName:         subject.DisplayName,
		Description:         subject.Description,
		CredentialSubjectID: subject.CredentialSubjectID,
		CreatedBySubjectID:  subject.CreatedBySubjectID,
		CreatedAt:           subject.CreatedAt,
		UpdatedAt:           subject.UpdatedAt,
		DeletedAt:           subject.DeletedAt,
	}
}

func managedSubjectIDFromCreateRequest(req createManagedSubjectRequest) (string, error) {
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = coredata.ManagedSubjectKindServiceAccount
	}
	if kind != coredata.ManagedSubjectKindServiceAccount {
		return "", fmt.Errorf("kind must be %q", coredata.ManagedSubjectKindServiceAccount)
	}
	if subjectID := strings.TrimSpace(req.SubjectID); subjectID != "" {
		return canonicalServiceAccountSubjectID(subjectID)
	}
	id := strings.TrimSpace(req.ID)
	if !validManagedSubjectLocalID(id) {
		return "", errors.New("id must contain only letters, numbers, dots, underscores, and hyphens")
	}
	return coredata.ManagedSubjectKindServiceAccount + ":" + id, nil
}

func canonicalServiceAccountSubjectID(subjectID string) (string, error) {
	kind, id, ok := core.ParseSubjectID(subjectID)
	if !ok || kind != coredata.ManagedSubjectKindServiceAccount || !validManagedSubjectLocalID(id) {
		return "", fmt.Errorf("subjectId must be a canonical %s:<id> subject ID", coredata.ManagedSubjectKindServiceAccount)
	}
	return kind + ":" + id, nil
}

func validManagedSubjectLocalID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '.', ch == '_', ch == '-':
		default:
			return false
		}
	}
	return true
}

func managedSubjectManagementRole(role string) bool {
	switch strings.TrimSpace(role) {
	case authorization.ProviderManagedSubjectRelationViewer,
		authorization.ProviderManagedSubjectRelationEditor,
		authorization.ProviderManagedSubjectRelationAdmin:
		return true
	default:
		return false
	}
}

func managedSubjectPluginRoleCanDelegate(callerRole, requestedRole string) bool {
	callerRole = strings.TrimSpace(callerRole)
	requestedRole = strings.TrimSpace(requestedRole)
	if callerRole == requestedRole {
		return true
	}
	callerRank, callerOK := managedSubjectRoleRank(callerRole)
	requestedRank, requestedOK := managedSubjectRoleRank(requestedRole)
	return callerOK && requestedOK && callerRank >= requestedRank
}

func managedSubjectRoleRank(role string) (int, bool) {
	switch strings.TrimSpace(role) {
	case authorization.ProviderManagedSubjectRelationViewer:
		return 1, true
	case authorization.ProviderManagedSubjectRelationEditor:
		return 2, true
	case authorization.ProviderManagedSubjectRelationAdmin:
		return 3, true
	default:
		return 0, false
	}
}

func managedSubjectRoleSortKey(role string) string {
	switch strings.TrimSpace(role) {
	case authorization.ProviderManagedSubjectRelationAdmin:
		return "0:admin"
	case authorization.ProviderManagedSubjectRelationEditor:
		return "1:editor"
	case authorization.ProviderManagedSubjectRelationViewer:
		return "2:viewer"
	default:
		return "9:" + role
	}
}

func managedSubjectActionRoles(action string) []string {
	switch strings.TrimSpace(action) {
	case authorization.ProviderManagedSubjectActionView:
		return []string{
			authorization.ProviderManagedSubjectRelationViewer,
			authorization.ProviderManagedSubjectRelationEditor,
			authorization.ProviderManagedSubjectRelationAdmin,
		}
	case authorization.ProviderManagedSubjectActionConnect:
		return []string{
			authorization.ProviderManagedSubjectRelationEditor,
			authorization.ProviderManagedSubjectRelationAdmin,
		}
	case authorization.ProviderManagedSubjectActionManage,
		authorization.ProviderManagedSubjectActionCreateToken,
		authorization.ProviderManagedSubjectActionGrant:
		return []string{authorization.ProviderManagedSubjectRelationAdmin}
	default:
		return []string{strings.TrimSpace(action)}
	}
}
