package server

import (
	"context"
	"errors"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/authorization"
)

var errExternalIdentityAlreadyLinked = errors.New("external identity already linked")

func (s *Server) syncStoredCredentialAuthorization(ctx context.Context, tok *core.ExternalCredential) error {
	if s.authorizationProvider == nil || tok == nil {
		return nil
	}
	ref, ok, err := externalIdentityRefFromMetadataJSON(tok.MetadataJSON)
	if err != nil || !ok {
		return err
	}
	return s.ensureExternalIdentityLink(ctx, strings.TrimSpace(tok.SubjectID), ref)
}

func (s *Server) unlinkStoredCredentialAuthorization(ctx context.Context, tok *core.ExternalCredential) error {
	if s.authorizationProvider == nil || tok == nil {
		return nil
	}
	ref, ok, err := externalIdentityRefFromMetadataJSON(tok.MetadataJSON)
	if err != nil || !ok {
		return err
	}
	stillClaimed, err := s.subjectHasOtherExternalIdentityLink(ctx, strings.TrimSpace(tok.SubjectID), ref, strings.TrimSpace(tok.ID))
	if err != nil || stillClaimed {
		return err
	}
	return s.removeExternalIdentityLink(ctx, strings.TrimSpace(tok.SubjectID), ref)
}

func (s *Server) subjectHasOtherExternalIdentityLink(ctx context.Context, subjectID string, ref externalIdentityRef, skipCredentialID string) (bool, error) {
	if coredata.ExternalCredentialProviderMissing(s.externalCredentials) || subjectID == "" {
		return false, nil
	}
	ref = normalizeExternalIdentityRef(ref)
	tokens, err := s.externalCredentials.ListCredentials(ctx, subjectID)
	if err != nil {
		return false, err
	}
	for _, candidate := range tokens {
		if candidate == nil || strings.TrimSpace(candidate.ID) == skipCredentialID {
			continue
		}
		candidateRef, ok, err := externalIdentityRefFromMetadataJSON(candidate.MetadataJSON)
		if err != nil {
			return false, err
		}
		if !ok {
			continue
		}
		candidateRef = normalizeExternalIdentityRef(candidateRef)
		if candidateRef == ref {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) ensureExternalIdentityLink(ctx context.Context, subjectID string, ref externalIdentityRef) error {
	if subjectID == "" {
		return nil
	}
	if err := validateExternalIdentityRef(ref); err != nil {
		return err
	}
	resourceID := externalIdentityResourceID(ref)
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Relation: externalIdentityLinkRelation,
		Resource: &core.ResourceRef{
			Type: authorization.ProviderResourceTypeExternalIdentity,
			Id:   resourceID,
		},
	})
	if err != nil {
		return err
	}

	currentSubjectLinked := false
	otherSubjectLinked := false
	for _, rel := range relationships {
		if rel == nil || rel.GetSubject() == nil {
			continue
		}
		if externalIdentityRelationshipSubjectMatches(rel.GetSubject(), subjectID) {
			currentSubjectLinked = true
			continue
		}
		otherSubjectLinked = true
	}
	if currentSubjectLinked {
		return nil
	}
	if otherSubjectLinked {
		return errExternalIdentityAlreadyLinked
	}

	modelID, err := s.managedAuthorizationModelID(ctx)
	if err != nil {
		return err
	}
	return s.authorizationProvider.WriteRelationships(ctx, &core.WriteRelationshipsRequest{
		Writes: []*core.Relationship{{
			Subject: &core.SubjectRef{
				Type: authorization.ProviderSubjectTypeSubject,
				Id:   subjectID,
			},
			Relation: externalIdentityLinkRelation,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   resourceID,
			},
		}},
		ModelId: modelID,
	})
}

func (s *Server) removeExternalIdentityLink(ctx context.Context, subjectID string, ref externalIdentityRef) error {
	if subjectID == "" {
		return nil
	}
	if err := validateExternalIdentityRef(ref); err != nil {
		return err
	}
	resourceID := externalIdentityResourceID(ref)
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Relation: externalIdentityLinkRelation,
		Resource: &core.ResourceRef{
			Type: authorization.ProviderResourceTypeExternalIdentity,
			Id:   resourceID,
		},
	})
	if err != nil {
		return err
	}

	target := make([]*core.Relationship, 0, 1)
	for _, rel := range relationships {
		if rel == nil || rel.GetSubject() == nil {
			continue
		}
		if externalIdentityRelationshipSubjectMatches(rel.GetSubject(), subjectID) {
			target = append(target, rel)
		}
	}
	if len(target) == 0 {
		return nil
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

func externalIdentityRelationshipSubjectMatches(subject *core.SubjectRef, subjectID string) bool {
	if subject == nil || strings.TrimSpace(subject.GetId()) != subjectID {
		return false
	}
	switch strings.TrimSpace(subject.GetType()) {
	case authorization.ProviderSubjectTypeSubject, authorization.ProviderSubjectTypeUser:
		return true
	default:
		return false
	}
}
