package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type putAuthorizationDynamicFragmentRequest struct {
	Owner           coredata.AuthorizationFragmentOwner                 `json:"owner"`
	ResourceTypes   map[string]json.RawMessage                          `json:"resourceTypes"`
	Relationships   []coredata.AuthorizationDynamicFragmentRelationship `json:"relationships"`
	ExpectedVersion *int64                                              `json:"expectedVersion"`
}

func (s *Server) listAdminAuthorizationGrants(w http.ResponseWriter, r *http.Request) {
	if !s.ensureAuthorizationDynamicFragmentStore(w) {
		return
	}
	if err := s.ensureAuthorizationDynamicFragmentsBackfilled(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to backfill authorization grants")
		return
	}
	grants, err := s.authzFragments.ListFragments(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list authorization grants")
		return
	}
	writeJSON(w, http.StatusOK, grants)
}

func (s *Server) getAdminAuthorizationGrant(w http.ResponseWriter, r *http.Request) {
	if !s.ensureAuthorizationDynamicFragmentStore(w) {
		return
	}
	if err := s.ensureAuthorizationDynamicFragmentsBackfilled(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to backfill authorization grants")
		return
	}
	id, err := decodedURLParam(r, "grantID")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fragment, err := s.authzFragments.GetFragment(r.Context(), id)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "authorization grant not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read authorization grant")
		return
	}
	writeJSON(w, http.StatusOK, fragment)
}

func (s *Server) putAdminAuthorizationGrant(w http.ResponseWriter, r *http.Request) {
	if !s.ensureAuthorizationDynamicFragmentStore(w) {
		return
	}
	if err := s.ensureAuthorizationDynamicFragmentsBackfilled(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to backfill authorization grants")
		return
	}
	id, err := decodedURLParam(r, "grantID")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req putAuthorizationDynamicFragmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Owner.Kind == "" && strings.HasPrefix(id, "app/") {
		req.Owner = coredata.AuthorizationAppFragmentOwner(strings.TrimPrefix(id, "app/"))
	}
	if req.Owner.Kind == "" && id == coredata.AuthorizationFragmentOwnerKindGlobal {
		req.Owner = coredata.AuthorizationGlobalFragmentOwner()
	}
	if expected := coredataFragmentID(req.Owner); expected != "" && expected != id {
		writeError(w, http.StatusBadRequest, "fragment id does not match owner")
		return
	}
	if !s.ensureAuthorizationFragmentWriteAccess(w, r, req.Owner) {
		return
	}
	fragment, err := s.authzFragments.PutFragment(r.Context(), &coredata.AuthorizationDynamicFragment{
		ID:            id,
		Owner:         req.Owner,
		ResourceTypes: req.ResourceTypes,
		Relationships: req.Relationships,
	}, coredata.AuthorizationDynamicFragmentUpdate{
		ExpectedVersion: req.ExpectedVersion,
		Audit:           s.authorizationFragmentAuditMetadata(r.Context(), "admin_api_put_fragment"),
	})
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "version mismatch") {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	s.auditAuthorizationFragmentMutation(r.Context(), "authorization.fragment.put", fragment, nil)
	if err := s.reloadAuthorizationState(r.Context()); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":    "persisted_pending_reload",
			"persisted": true,
			"reloaded":  false,
			"fragment":  fragment,
		})
		return
	}
	writeJSON(w, http.StatusOK, fragment)
}

func (s *Server) deleteAdminAuthorizationGrant(w http.ResponseWriter, r *http.Request) {
	if !s.ensureAuthorizationDynamicFragmentStore(w) {
		return
	}
	if err := s.ensureAuthorizationDynamicFragmentsBackfilled(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to backfill authorization grants")
		return
	}
	id, err := decodedURLParam(r, "grantID")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	owner := authorizationFragmentOwnerFromID(id)
	if !s.ensureAuthorizationFragmentWriteAccess(w, r, owner) {
		return
	}
	fragment, getErr := s.authzFragments.GetFragment(r.Context(), id)
	if getErr != nil {
		if errors.Is(getErr, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "authorization grant not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read authorization grant")
		return
	}
	if err := s.authzFragments.DeleteFragment(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete authorization grant")
		return
	}
	s.auditAuthorizationFragmentMutation(r.Context(), "authorization.fragment.delete", fragment, nil)
	if err := s.reloadAuthorizationState(r.Context()); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":    "deleted_pending_reload",
			"persisted": true,
			"reloaded":  false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "deleted",
		"persisted": true,
		"reloaded":  true,
	})
}

func (s *Server) ensureAuthorizationFragmentWriteAccess(w http.ResponseWriter, r *http.Request, owner coredata.AuthorizationFragmentOwner) bool {
	owner.Kind = strings.TrimSpace(owner.Kind)
	owner.App = strings.TrimSpace(owner.App)
	access := invocation.AccessContextFromContext(r.Context())
	if s.adminRoleCanMutate(access.Role) {
		return true
	}
	if owner.Kind == coredata.AuthorizationFragmentOwnerKindApp && owner.App != "" {
		p := principal.FromContext(r.Context())
		pluginAccess, allowed := s.authorizer.ResolveAccess(r.Context(), p, owner.App)
		if allowed && adminAuthorizationPluginRoleCanMutate(pluginAccess.Role) {
			return true
		}
	}
	writeError(w, http.StatusForbidden, "authorization grant changes require admin access or app admin access")
	return false
}

func (s *Server) ensureAuthorizationDynamicFragmentStore(w http.ResponseWriter) bool {
	if s.authzFragments == nil {
		writeError(w, http.StatusServiceUnavailable, "dynamic authorization grants require indexeddb source state")
		return false
	}
	return true
}

func (s *Server) ensureAuthorizationDynamicFragmentsBackfilled(ctx context.Context) error {
	if s.authzFragments == nil || s.authorizationProvider == nil {
		return nil
	}
	s.authzFragmentBackfillMu.Lock()
	defer s.authzFragmentBackfillMu.Unlock()
	if s.authzFragmentBackfilled {
		return nil
	}
	if err := s.backfillAuthorizationDynamicFragments(ctx); err != nil {
		return err
	}
	s.authzFragmentBackfilled = true
	return nil
}

func (s *Server) backfillAuthorizationDynamicFragments(ctx context.Context) error {
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
	})
	if err != nil {
		return err
	}
	for _, rel := range relationships {
		fragmentRelationship, owner, ok := authorizationDynamicFragmentRelationshipFromProvider(rel)
		if !ok {
			continue
		}
		if _, err := s.authzFragments.UpsertRelationship(ctx, owner, fragmentRelationship, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "backfill_provider_dynamic_relationship"}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) upsertAuthorizationDynamicFragmentRelationship(ctx context.Context, owner coredata.AuthorizationFragmentOwner, relationship coredata.AuthorizationDynamicFragmentRelationship, reason string) (*coredata.AuthorizationDynamicFragment, error) {
	if s.authzFragments == nil {
		return nil, nil
	}
	if err := s.ensureAuthorizationDynamicFragmentsBackfilled(ctx); err != nil {
		return nil, err
	}
	fragment, err := s.authzFragments.ReplaceSubjectResourceRelationships(ctx, owner, relationship, s.authorizationFragmentAuditMetadata(ctx, reason))
	if err != nil {
		return nil, err
	}
	s.auditAuthorizationFragmentMutation(ctx, "authorization.fragment.relationship.upsert", fragment, &relationship)
	return fragment, nil
}

func (s *Server) deleteAuthorizationDynamicFragmentSubjectResourceRelationships(ctx context.Context, owner coredata.AuthorizationFragmentOwner, subject coredata.AuthorizationDynamicFragmentSubject, resource coredata.AuthorizationDynamicFragmentResource, reason string) (bool, *coredata.AuthorizationDynamicFragment, error) {
	if s.authzFragments == nil {
		return false, nil, nil
	}
	if err := s.ensureAuthorizationDynamicFragmentsBackfilled(ctx); err != nil {
		return false, nil, err
	}
	deleted, fragment, err := s.authzFragments.DeleteSubjectResourceRelationships(ctx, owner, subject, resource, s.authorizationFragmentAuditMetadata(ctx, reason))
	if err != nil {
		return false, nil, err
	}
	if deleted {
		s.auditAuthorizationFragmentMutation(ctx, "authorization.fragment.relationship.delete", fragment, &coredata.AuthorizationDynamicFragmentRelationship{
			Subject:  subject,
			Resource: resource,
		})
	}
	return deleted, fragment, nil
}

func (s *Server) authorizationFragmentAuditMetadata(ctx context.Context, reason string) coredata.AuthorizationDynamicFragmentAuditMetadata {
	meta := coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: reason}
	if p := principal.FromContext(ctx); p != nil {
		meta.UpdatedBySubjectID = p.SubjectID
		meta.UpdatedByAuthSource = p.AuthSource()
	}
	return meta
}

func (s *Server) auditAuthorizationFragmentMutation(ctx context.Context, operation string, fragment *coredata.AuthorizationDynamicFragment, relationship *coredata.AuthorizationDynamicFragmentRelationship) {
	if s.auditSink == nil {
		return
	}
	entry := core.AuditEntry{
		Source:    "admin",
		Operation: operation,
		Allowed:   true,
	}
	if fragment != nil {
		entry.TargetKind = "authorization_dynamic_fragment"
		entry.TargetID = fragment.ID
		if fragment.Owner.Kind == coredata.AuthorizationFragmentOwnerKindApp {
			entry.Provider = fragment.Owner.App
		}
	}
	if p := principal.FromContext(ctx); p != nil {
		entry.SubjectID = p.SubjectID
	}
	if relationship != nil {
		entry.TargetName = fmt.Sprintf("%s:%s#%s@%s:%s", relationship.Resource.Type, relationship.Resource.ID, relationship.Relation, relationship.Subject.Type, relationship.Subject.ID)
	}
	s.auditSink.Log(ctx, entry)
}

func authorizationDynamicFragmentRelationshipFromProvider(rel *core.Relationship) (coredata.AuthorizationDynamicFragmentRelationship, coredata.AuthorizationFragmentOwner, bool) {
	if rel == nil || authorization.RelationshipSubject(rel) == nil || rel.GetResource() == nil {
		return coredata.AuthorizationDynamicFragmentRelationship{}, coredata.AuthorizationFragmentOwner{}, false
	}
	resource := rel.GetResource()
	switch resource.GetType() {
	case authorization.ProviderResourceTypeAppDynamic:
		app := strings.TrimSpace(resource.GetId())
		if app == "" {
			return coredata.AuthorizationDynamicFragmentRelationship{}, coredata.AuthorizationFragmentOwner{}, false
		}
		return authorizationDynamicFragmentRelationshipFromCore(rel), coredata.AuthorizationAppFragmentOwner(app), true
	case authorization.ProviderResourceTypeAdminDynamic:
		if strings.TrimSpace(resource.GetId()) != authorization.ProviderResourceIDAdminDynamicGlobal {
			return coredata.AuthorizationDynamicFragmentRelationship{}, coredata.AuthorizationFragmentOwner{}, false
		}
		return authorizationDynamicFragmentRelationshipFromCore(rel), coredata.AuthorizationGlobalFragmentOwner(), true
	default:
		return coredata.AuthorizationDynamicFragmentRelationship{}, coredata.AuthorizationFragmentOwner{}, false
	}
}

func authorizationDynamicFragmentRelationshipFromCore(rel *core.Relationship) coredata.AuthorizationDynamicFragmentRelationship {
	subject := authorization.RelationshipSubject(rel)
	properties := map[string]string{}
	for key, value := range rel.GetProperties().GetFields() {
		properties[key] = fmt.Sprint(value.AsInterface())
	}
	if len(properties) == 0 {
		properties = nil
	}
	return coredata.AuthorizationDynamicFragmentRelationship{
		Subject: coredata.AuthorizationDynamicFragmentSubject{
			Type: strings.TrimSpace(subject.GetType()),
			ID:   strings.TrimSpace(subject.GetId()),
		},
		Relation: strings.TrimSpace(rel.GetRelation()),
		Resource: coredata.AuthorizationDynamicFragmentResource{
			Type: strings.TrimSpace(rel.GetResource().GetType()),
			ID:   strings.TrimSpace(rel.GetResource().GetId()),
		},
		Target:     authorizationDynamicFragmentTargetFromCore(rel.GetTarget()),
		Properties: properties,
	}
}

func authorizationDynamicFragmentTargetFromCore(target *core.RelationshipTargetRef) coredata.AuthorizationDynamicFragmentTarget {
	if target == nil {
		return coredata.AuthorizationDynamicFragmentTarget{}
	}
	if subject := target.GetSubject(); subject != nil {
		return coredata.AuthorizationDynamicFragmentTarget{
			Subject: &coredata.AuthorizationDynamicFragmentSubject{
				Type: strings.TrimSpace(subject.GetType()),
				ID:   strings.TrimSpace(subject.GetId()),
			},
		}
	}
	if resource := target.GetResource(); resource != nil {
		return coredata.AuthorizationDynamicFragmentTarget{
			Resource: &coredata.AuthorizationDynamicFragmentResource{
				Type: strings.TrimSpace(resource.GetType()),
				ID:   strings.TrimSpace(resource.GetId()),
			},
		}
	}
	if subjectSet := target.GetSubjectSet(); subjectSet != nil {
		resource := subjectSet.GetResource()
		return coredata.AuthorizationDynamicFragmentTarget{
			SubjectSet: &coredata.AuthorizationDynamicFragmentSubjectSet{
				Resource: coredata.AuthorizationDynamicFragmentResource{
					Type: strings.TrimSpace(resource.GetType()),
					ID:   strings.TrimSpace(resource.GetId()),
				},
				Relation: strings.TrimSpace(subjectSet.GetRelation()),
			},
		}
	}
	return coredata.AuthorizationDynamicFragmentTarget{}
}

func coredataFragmentID(owner coredata.AuthorizationFragmentOwner) string {
	switch strings.TrimSpace(owner.Kind) {
	case coredata.AuthorizationFragmentOwnerKindGlobal:
		return coredata.AuthorizationFragmentOwnerKindGlobal
	case coredata.AuthorizationFragmentOwnerKindApp:
		if strings.TrimSpace(owner.App) == "" {
			return ""
		}
		return "app/" + strings.TrimSpace(owner.App)
	default:
		return ""
	}
}

func authorizationFragmentOwnerFromID(id string) coredata.AuthorizationFragmentOwner {
	id = strings.TrimSpace(id)
	if id == coredata.AuthorizationFragmentOwnerKindGlobal {
		return coredata.AuthorizationGlobalFragmentOwner()
	}
	if strings.HasPrefix(id, "app/") {
		return coredata.AuthorizationAppFragmentOwner(strings.TrimSpace(strings.TrimPrefix(id, "app/")))
	}
	return coredata.AuthorizationFragmentOwner{}
}
