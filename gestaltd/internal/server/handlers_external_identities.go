package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type createCurrentUserExternalIdentityRequest struct {
	LinkToken string `json:"linkToken"`
}

type externalIdentityLinkInfo struct {
	ID               string              `json:"id"`
	ExternalIdentity externalIdentityRef `json:"externalIdentity"`
}

func (s *Server) listCurrentUserExternalIdentities(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("external identity list failed")
	defer func() {
		s.auditHTTPEvent(r.Context(), PrincipalFromContext(r.Context()), "", "external_identity.list", auditAllowed, auditErr)
	}()

	userID, ok := s.resolveCurrentUserExternalIdentityUserID(w, r)
	if !ok {
		return
	}

	links, err := s.currentUserExternalIdentityLinks(r.Context(), userID)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusInternalServerError, "failed to list external identities")
		return
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, links)
}

func (s *Server) createCurrentUserExternalIdentity(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("external identity link failed")
	auditTarget := auditTarget{Kind: auditTargetKindExternalIdentity}
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "external_identity.link", auditAllowed, auditErr, auditTarget)
	}()

	userID, ok := s.resolveCurrentUserExternalIdentityUserID(w, r)
	if !ok {
		return
	}
	if s.encryptor == nil {
		auditErr = errors.New("external identity link tokens are unavailable")
		writeError(w, http.StatusServiceUnavailable, "external identity link tokens are unavailable")
		return
	}

	var req createCurrentUserExternalIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.LinkToken = strings.TrimSpace(req.LinkToken)
	if req.LinkToken == "" {
		auditErr = errors.New("linkToken is required")
		writeError(w, http.StatusBadRequest, "linkToken is required")
		return
	}

	state, err := decodeExternalIdentityLinkToken(s.encryptor, req.LinkToken, s.now())
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, "linkToken is invalid or expired")
		return
	}
	currentSubjectID := principal.UserSubjectID(userID)
	if state.SubjectID != currentSubjectID {
		auditErr = errors.New("linkToken does not match the current user")
		writeError(w, http.StatusForbidden, "linkToken does not match the current user")
		return
	}
	ref := externalIdentityLinkInfo{
		ID:               externalIdentityLinkID(state.ExternalIdentity),
		ExternalIdentity: state.ExternalIdentity,
	}
	auditTarget = externalIdentityAuditTarget(ref.ExternalIdentity.Type, ref.ExternalIdentity.ID)

	status, err := s.linkCurrentUserExternalIdentity(r.Context(), userID, ref.ExternalIdentity)
	if err != nil {
		auditErr = err
		switch {
		case errors.Is(err, errExternalIdentityAlreadyLinked):
			writeError(w, http.StatusConflict, "external identity is already linked")
		default:
			writeError(w, http.StatusInternalServerError, "failed to link external identity")
		}
		return
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, status, ref)
}

func (s *Server) deleteCurrentUserExternalIdentity(w http.ResponseWriter, r *http.Request) {
	linkID := strings.TrimSpace(chi.URLParam(r, "linkID"))
	auditAllowed := false
	auditErr := errors.New("external identity unlink failed")
	auditTarget := auditTarget{Kind: auditTargetKindExternalIdentity, ID: linkID}
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), "", "external_identity.unlink", auditAllowed, auditErr, auditTarget)
	}()

	userID, ok := s.resolveCurrentUserExternalIdentityUserID(w, r)
	if !ok {
		return
	}
	ref, err := decodeExternalIdentityLinkID(linkID)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, "linkID is invalid")
		return
	}
	auditTarget = externalIdentityAuditTarget(ref.Type, ref.ID)

	if err := s.unlinkCurrentUserExternalIdentity(r.Context(), userID, ref); err != nil {
		auditErr = err
		switch {
		case errors.Is(err, core.ErrNotFound):
			writeError(w, http.StatusNotFound, "external identity not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to unlink external identity")
		}
		return
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) resolveCurrentUserExternalIdentityUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.authorizationProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "external identities require an authorization provider")
		return "", false
	}
	userID, err := s.resolveUserID(w, r)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(userID), true
}

func (s *Server) currentUserExternalIdentityLinks(ctx context.Context, userID string) ([]externalIdentityLinkInfo, error) {
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Subject: &core.SubjectRef{
			Type: "user",
			Id:   principal.UserSubjectID(userID),
		},
		Relation: externalIdentityLinkRelation,
	})
	if err != nil {
		return nil, err
	}
	links := make([]externalIdentityLinkInfo, 0, len(relationships))
	seen := make(map[string]struct{}, len(relationships))
	for _, rel := range relationships {
		if rel == nil || rel.GetResource() == nil {
			continue
		}
		if strings.TrimSpace(rel.GetResource().GetType()) != authorization.ProviderResourceTypeExternalIdentity {
			continue
		}
		ref, err := decodeExternalIdentityLinkID(strings.TrimSpace(rel.GetResource().GetId()))
		if err != nil {
			continue
		}
		link := externalIdentityLinkInfo{
			ID:               externalIdentityLinkID(ref),
			ExternalIdentity: ref,
		}
		if link.ID == "" {
			continue
		}
		key := link.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		links = append(links, link)
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].ExternalIdentity.Type != links[j].ExternalIdentity.Type {
			return links[i].ExternalIdentity.Type < links[j].ExternalIdentity.Type
		}
		if links[i].ExternalIdentity.ID != links[j].ExternalIdentity.ID {
			return links[i].ExternalIdentity.ID < links[j].ExternalIdentity.ID
		}
		return links[i].ID < links[j].ID
	})
	return links, nil
}

var errExternalIdentityAlreadyLinked = errors.New("external identity already linked")

func (s *Server) linkCurrentUserExternalIdentity(ctx context.Context, userID string, ref externalIdentityRef) (int, error) {
	linkID := externalIdentityLinkID(ref)
	if linkID == "" {
		return 0, errors.New("external identity is invalid")
	}
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Relation: externalIdentityLinkRelation,
		Resource: &core.ResourceRef{
			Type: authorization.ProviderResourceTypeExternalIdentity,
			Id:   linkID,
		},
	})
	if err != nil {
		return 0, err
	}

	subjectID := principal.UserSubjectID(userID)
	currentUserLinked := false
	otherUserLinked := false
	for _, rel := range relationships {
		if rel == nil || rel.GetSubject() == nil {
			continue
		}
		if strings.TrimSpace(rel.GetSubject().GetType()) == "user" && strings.TrimSpace(rel.GetSubject().GetId()) == subjectID {
			currentUserLinked = true
			continue
		}
		otherUserLinked = true
	}
	if currentUserLinked {
		return http.StatusOK, nil
	}
	if otherUserLinked {
		return 0, errExternalIdentityAlreadyLinked
	}

	modelID, err := s.managedAuthorizationModelID(ctx)
	if err != nil {
		return 0, err
	}
	if err := s.authorizationProvider.WriteRelationships(ctx, &core.WriteRelationshipsRequest{
		Writes: []*core.Relationship{{
			Subject: &core.SubjectRef{
				Type: "user",
				Id:   subjectID,
			},
			Relation: externalIdentityLinkRelation,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   linkID,
			},
		}},
		ModelId: modelID,
	}); err != nil {
		return 0, err
	}
	return http.StatusCreated, nil
}

func (s *Server) unlinkCurrentUserExternalIdentity(ctx context.Context, userID string, ref externalIdentityRef) error {
	linkID := externalIdentityLinkID(ref)
	if linkID == "" {
		return errors.New("external identity is invalid")
	}
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Relation: externalIdentityLinkRelation,
		Resource: &core.ResourceRef{
			Type: authorization.ProviderResourceTypeExternalIdentity,
			Id:   linkID,
		},
	})
	if err != nil {
		return err
	}

	subjectID := principal.UserSubjectID(userID)
	target := make([]*core.Relationship, 0, 1)
	for _, rel := range relationships {
		if rel == nil || rel.GetSubject() == nil {
			continue
		}
		if strings.TrimSpace(rel.GetSubject().GetType()) == "user" && strings.TrimSpace(rel.GetSubject().GetId()) == subjectID {
			target = append(target, rel)
		}
	}
	if len(target) == 0 {
		return core.ErrNotFound
	}

	modelID, err := s.managedAuthorizationModelID(ctx)
	if err != nil {
		return err
	}
	return s.authorizationProvider.WriteRelationships(ctx, &core.WriteRelationshipsRequest{
		Deletes: relationshipKeys(target),
		ModelId: modelID,
	})
}
