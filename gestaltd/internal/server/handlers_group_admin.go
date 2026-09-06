package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const (
	groupAuthorizationResourceType = "group"
	groupAdminRelation             = "admin"
	groupMemberRelation            = "member"
)

type groupAdminSummary struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	MemberCount int    `json:"memberCount"`
	ScimManaged bool   `json:"scimManaged"`
	Editable    bool   `json:"editable"`
	CanAdmin    bool   `json:"canAdmin"`
}

type groupAdminCreateRequest struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type groupAdminCreateResponse struct {
	Group groupAdminSummary `json:"group"`
}

type groupAdminMemberSetRequest struct {
	SubjectID string `json:"subjectId"`
}

type groupAdminMemberRemoveRequest struct {
	SubjectID string `json:"subjectId"`
}

func (s *Server) mountGroupAdminRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("group"), s.groupAdminListAuthorizationMiddleware).
		Get("/groups", s.listGroupAdminGroups)
	r.With(s.pluginRouteAuthMiddleware("group"), s.groupAdminCreateAuthorizationMiddleware).
		Post("/groups", s.createGroupAdminGroup)
	r.With(s.pluginRouteAuthMiddleware("group"), s.groupAdminShowAuthorizationMiddleware).
		Get("/groups/{group}", s.getGroupAdminGroup)
	r.With(s.pluginRouteAuthMiddleware("group"), s.groupAdminAuthorizationMiddleware).
		Get("/groups/{group}/admin/members", s.listGroupAdminMembers)
	r.With(s.pluginRouteAuthMiddleware("group"), s.groupAdminAuthorizationMiddleware).
		Post("/groups/{group}/admin/members", s.setGroupAdminMember)
	r.With(s.pluginRouteAuthMiddleware("group"), s.groupAdminAuthorizationMiddleware).
		Delete("/groups/{group}/admin/members", s.removeGroupAdminMember)
}

func (s *Server) groupResource(groupID string) *proto.Resource {
	return &proto.Resource{Type: groupAuthorizationResourceType, Id: strings.TrimSpace(groupID)}
}

func (s *Server) isScimManagedGroup(groupID string) bool {
	if s == nil || s.scimManagedGroupIDs == nil {
		return false
	}
	_, ok := s.scimManagedGroupIDs[strings.TrimSpace(groupID)]
	return ok
}

func (s *Server) resolveGroupAdminSubjectID(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := PrincipalFromContext(r.Context())
	if p == nil {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return "", false
	}
	if err := requireUserCaller(w, p); err != nil {
		return "", false
	}
	subjectID, err := principal.ResolveAuthorizationSubjectID(r.Context(), s.credentialUserResolver(), p)
	switch {
	case errors.Is(err, principal.ErrCredentialSubjectRequired):
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return "", false
	case errors.Is(err, principal.ErrOpaqueCredentialSubject):
		writeError(w, http.StatusForbidden, "group access denied")
		return "", false
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return "", false
	}
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return "", false
	}
	return subjectID, true
}

func (s *Server) hasExplicitGroupAdmin(ctx context.Context, subjectID, groupID string) (bool, error) {
	if s == nil || s.authorization == nil {
		return false, errors.New("authorization is unavailable")
	}
	groupID = strings.TrimSpace(groupID)
	decision, err := s.checkResourceAccess(ctx, invocation.ResourceAccessRequest{
		SubjectID:    subjectID,
		Action:       groupID,
		Resource:     s.groupResource(groupID),
		AllowedRoles: []string{groupAdminRelation},
	})
	if err != nil {
		return false, err
	}
	return decision.Allowed && decision.Role == groupAdminRelation, nil
}

func (s *Server) hasAuthorizationViewer(ctx context.Context, subjectID string) (bool, error) {
	if s == nil || s.authorization == nil {
		return false, errors.New("authorization is unavailable")
	}
	allowed, err := invocation.CheckSubjectAccess(ctx, s.authorization, invocation.SubjectAccessRequest(
		subjectID,
		"viewer",
		&proto.Resource{Type: "authorization", Id: "authorization"},
	))
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func (s *Server) hasGestaltAdmin(ctx context.Context, subjectID string) (bool, error) {
	if s == nil || s.authorization == nil {
		return false, errors.New("authorization is unavailable")
	}
	allowed, err := invocation.CheckSubjectAccess(ctx, s.authorization, invocation.SubjectAccessRequest(
		subjectID,
		"admin",
		&proto.Resource{Type: "gestalt", Id: "gestalt"},
	))
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func (s *Server) hasAuthorizationAdmin(ctx context.Context, subjectID string) (bool, error) {
	if s == nil || s.authorization == nil {
		return false, errors.New("authorization is unavailable")
	}
	for _, action := range []string{"admin", "authorization"} {
		allowed, err := invocation.CheckSubjectAccess(ctx, s.authorization, invocation.SubjectAccessRequest(
			subjectID,
			action,
			&proto.Resource{Type: "authorization", Id: "authorization"},
		))
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) groupAdminAuthorizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorization == nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		subjectID, ok := s.resolveGroupAdminSubjectID(w, r)
		if !ok {
			return
		}
		groupID := strings.TrimSpace(chi.URLParam(r, "group"))
		if groupID == "" {
			writeError(w, http.StatusBadRequest, "group is required")
			return
		}
		allowed, err := s.hasExplicitGroupAdmin(r.Context(), subjectID, groupID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		if !allowed {
			globalAdmin, err := s.hasGestaltAdmin(r.Context(), subjectID)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
				return
			}
			if !globalAdmin {
				writeError(w, http.StatusForbidden, "group access denied")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) groupAdminListAuthorizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorization == nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		subjectID, ok := s.resolveGroupAdminSubjectID(w, r)
		if !ok {
			return
		}
		allowed, err := s.canListGroupAdminGroups(r.Context(), subjectID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "group access denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) groupAdminCreateAuthorizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorization == nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		subjectID, ok := s.resolveGroupAdminSubjectID(w, r)
		if !ok {
			return
		}
		allowed, err := s.canCreateGroupAdminGroup(r.Context(), subjectID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "group access denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) canListGroupAdminGroups(ctx context.Context, subjectID string) (bool, error) {
	if ok, err := s.hasAuthorizationViewer(ctx, subjectID); err != nil || ok {
		return ok, err
	}
	if ok, err := s.hasGestaltAdmin(ctx, subjectID); err != nil || ok {
		return ok, err
	}
	return s.hasAnyGroupAdmin(ctx, subjectID)
}

func (s *Server) canViewGroupAdminGroup(ctx context.Context, subjectID, groupID string) (bool, error) {
	if ok, err := s.hasAuthorizationViewer(ctx, subjectID); err != nil || ok {
		return ok, err
	}
	if ok, err := s.hasGestaltAdmin(ctx, subjectID); err != nil || ok {
		return ok, err
	}
	return s.hasExplicitGroupAdmin(ctx, subjectID, groupID)
}

func (s *Server) hasAnyGroupAdmin(ctx context.Context, subjectID string) (bool, error) {
	if s == nil || s.authorization == nil {
		return false, errors.New("authorization is unavailable")
	}
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return false, nil
	}
	resp, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{
		Filter: &proto.RelationshipFilter{
			ResourceType: groupAuthorizationResourceType,
			Relation:     groupAdminRelation,
			Target: &proto.RelationshipTarget{
				Kind: &proto.RelationshipTarget_Subject{
					Subject: &proto.Subject{Type: "subject", Id: subjectID},
				},
			},
		},
		PageSize: 1,
	})
	if err != nil {
		return false, err
	}
	return len(resp.GetRelationships()) > 0, nil
}

func (s *Server) canCreateGroupAdminGroup(ctx context.Context, subjectID string) (bool, error) {
	if ok, err := s.hasAuthorizationAdmin(ctx, subjectID); err != nil || ok {
		return ok, err
	}
	return s.hasGestaltAdmin(ctx, subjectID)
}

func (s *Server) listGroupIDs(ctx context.Context) ([]string, error) {
	seen := map[string]struct{}{}
	ids := make([]string, 0)
	pageToken := ""
	for {
		resp, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{
			Filter: &proto.RelationshipFilter{
				ResourceType: groupAuthorizationResourceType,
			},
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, relationship := range resp.GetRelationships() {
			if relationship == nil || relationship.GetTuple() == nil {
				continue
			}
			id := strings.TrimSpace(relationship.GetTuple().GetResource().GetId())
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return ids, nil
		}
	}
}

func (s *Server) countGroupMembers(ctx context.Context, groupID string) (int, error) {
	count := 0
	pageToken := ""
	resource := s.groupResource(groupID)
	for {
		resp, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{
			Filter: &proto.RelationshipFilter{
				Resource: resource,
				Relation: groupMemberRelation,
			},
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			return 0, err
		}
		count += len(resp.GetRelationships())
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return count, nil
		}
	}
}

func (s *Server) groupAdminShowAuthorizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorization == nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		subjectID, ok := s.resolveGroupAdminSubjectID(w, r)
		if !ok {
			return
		}
		groupID := strings.TrimSpace(chi.URLParam(r, "group"))
		if groupID == "" {
			writeError(w, http.StatusBadRequest, "group is required")
			return
		}
		allowed, err := s.canViewGroupAdminGroup(r.Context(), subjectID, groupID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "group access denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) getGroupAdminGroup(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "group"))
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "group is required")
		return
	}
	subjectID, ok := s.resolveGroupAdminSubjectID(w, r)
	if !ok {
		return
	}
	summary, err := s.groupAdminSummary(r.Context(), subjectID, groupID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) listGroupAdminGroups(w http.ResponseWriter, r *http.Request) {
	subjectID, ok := s.resolveGroupAdminSubjectID(w, r)
	if !ok {
		return
	}
	groupIDs, err := s.listGroupIDs(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	summaries := make([]groupAdminSummary, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		summary, err := s.groupAdminSummary(r.Context(), subjectID, groupID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
			return
		}
		summaries = append(summaries, summary)
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (s *Server) groupAdminSummary(ctx context.Context, subjectID, groupID string) (groupAdminSummary, error) {
	memberCount, err := s.countGroupMembers(ctx, groupID)
	if err != nil {
		return groupAdminSummary{}, err
	}
	canAdmin, err := s.hasExplicitGroupAdmin(ctx, subjectID, groupID)
	if err != nil {
		return groupAdminSummary{}, err
	}
	if !canAdmin {
		globalAdmin, err := s.hasGestaltAdmin(ctx, subjectID)
		if err != nil {
			return groupAdminSummary{}, err
		}
		canAdmin = globalAdmin
	}
	scimManaged := s.isScimManagedGroup(groupID)
	return groupAdminSummary{
		ID:          groupID,
		DisplayName: groupID,
		MemberCount: memberCount,
		ScimManaged: scimManaged,
		Editable:    !scimManaged,
		CanAdmin:    canAdmin && !scimManaged,
	}, nil
}

func (s *Server) createGroupAdminGroup(w http.ResponseWriter, r *http.Request) {
	subjectID, ok := s.resolveGroupAdminSubjectID(w, r)
	if !ok {
		return
	}
	var request groupAdminCreateRequest
	if !decodeStrictJSONBody(w, r, &request) {
		return
	}
	groupID := strings.TrimSpace(request.ID)
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	displayName := strings.TrimSpace(request.DisplayName)
	if displayName == "" {
		displayName = groupID
	}
	if err := s.addGroupAdminMemberRole(r.Context(), groupID, groupAdminRelation, subjectID); err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	summary, err := s.groupAdminSummary(r.Context(), subjectID, groupID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	summary.DisplayName = displayName
	writeJSON(w, http.StatusCreated, groupAdminCreateResponse{Group: summary})
}

func (s *Server) listGroupAdminMembers(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "group"))
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "group is required")
		return
	}
	if s.isScimManagedGroup(groupID) {
		writeError(w, http.StatusForbidden, "group is read-only")
		return
	}
	rows, err := s.listAuthorizationMemberRows(r.Context(), s.groupResource(groupID))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	filtered := make([]appAdminMemberRow, 0, len(rows))
	for _, row := range rows {
		if row.Role != groupMemberRelation {
			continue
		}
		filtered = append(filtered, row)
	}
	writeJSON(w, http.StatusOK, s.projectAppAdminHumanMemberRows(r.Context(), filtered))
}

func (s *Server) setGroupAdminMember(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "group"))
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "group is required")
		return
	}
	if s.isScimManagedGroup(groupID) {
		writeError(w, http.StatusForbidden, "group is read-only")
		return
	}
	var request groupAdminMemberSetRequest
	if !decodeStrictJSONBody(w, r, &request) {
		return
	}
	subjectID := strings.TrimSpace(request.SubjectID)
	if subjectID == "" {
		writeError(w, http.StatusBadRequest, "subjectId is required")
		return
	}
	if err := validateAppAdminMemberSubjectID(subjectID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	recordAppAdminUITargetSubject(r.Context(), subjectID)
	if err := s.addGroupAdminMemberRole(r.Context(), groupID, groupMemberRelation, subjectID); err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"group":     groupID,
		"subjectId": subjectID,
		"role":      groupMemberRelation,
	})
}

func (s *Server) removeGroupAdminMember(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "group"))
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "group is required")
		return
	}
	if s.isScimManagedGroup(groupID) {
		writeError(w, http.StatusForbidden, "group is read-only")
		return
	}
	var request groupAdminMemberRemoveRequest
	if !decodeStrictJSONBody(w, r, &request) {
		return
	}
	subjectID := strings.TrimSpace(request.SubjectID)
	if subjectID == "" {
		writeError(w, http.StatusBadRequest, "subjectId is required")
		return
	}
	if err := validateAppAdminMemberSubjectID(subjectID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	recordAppAdminUITargetSubject(r.Context(), subjectID)
	if err := s.deleteGroupAdminMemberRole(r.Context(), groupID, groupMemberRelation, subjectID); err != nil {
		writeError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"group":         groupID,
		"subjectId":     subjectID,
		"removedRoles":  []string{groupMemberRelation},
	})
}

func (s *Server) addGroupAdminMemberRole(ctx context.Context, groupID, role, subjectID string) error {
	if s == nil || s.authorization == nil {
		return errors.New("authorization is unavailable")
	}
	_, err := s.authorization.AddRelationship(ctx, &proto.AddRelationshipRequest{
		Relationship: &proto.Relationship{
			Tuple:       groupAdminMemberRelationshipTuple(groupID, role, subjectID),
			SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
		},
	})
	return err
}

func (s *Server) deleteGroupAdminMemberRole(ctx context.Context, groupID, role, subjectID string) error {
	if s == nil || s.authorization == nil {
		return errors.New("authorization is unavailable")
	}
	_, err := s.authorization.DeleteRelationship(ctx, &proto.DeleteRelationshipRequest{
		RelationshipTuple: groupAdminMemberRelationshipTuple(groupID, role, subjectID),
	})
	return err
}

func groupAdminMemberRelationshipTuple(groupID, role, subjectID string) *proto.RelationshipTuple {
	return &proto.RelationshipTuple{
		Resource: &proto.Resource{Type: groupAuthorizationResourceType, Id: strings.TrimSpace(groupID)},
		Relation: strings.TrimSpace(role),
		Target: &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
				Type: "subject",
				Id:   strings.TrimSpace(subjectID),
			}},
		},
	}
}
