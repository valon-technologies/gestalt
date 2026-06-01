package server

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func (s *Server) upsertAuthorizationDynamicFragmentRelationship(ctx context.Context, owner coredata.AuthorizationFragmentOwner, relationship coredata.AuthorizationDynamicFragmentRelationship, reason string) (*coredata.AuthorizationDynamicFragment, error) {
	if s.authzFragments == nil {
		return nil, nil
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
		entry.AuthSource = p.AuthSource()
	}
	if relationship != nil {
		entry.TargetName = fmt.Sprintf("%s:%s#%s@%s:%s", relationship.Resource.Type, relationship.Resource.ID, relationship.Relation, relationship.Subject.Type, relationship.Subject.ID)
	}
	s.auditSink.Log(ctx, entry)
}
