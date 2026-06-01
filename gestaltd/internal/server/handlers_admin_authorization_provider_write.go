package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/authorization"
)

func (s *Server) deleteDynamicFragmentMembership(ctx context.Context, owner coredata.AuthorizationFragmentOwner, resource *core.ResourceRef, subjectID, reason string) (bool, *coredata.AuthorizationDynamicFragment, error) {
	return s.deleteAuthorizationDynamicFragmentSubjectResourceRelationships(ctx, owner, coredata.AuthorizationDynamicFragmentSubject{
		Type: authorization.ProviderSubjectTypeSubject,
		ID:   strings.TrimSpace(subjectID),
	}, coredata.AuthorizationDynamicFragmentResource{
		Type: resource.GetType(),
		ID:   resource.GetId(),
	}, reason)
}

func (s *Server) upsertManagedSubjectExternalIdentity(ctx context.Context, subjectID string, ref externalIdentityRef) error {
	if s.authorizationProvider == nil {
		return errAdminAuthorizationUnavailable
	}
	// Managed subject assumptions are Zanzibar authorization tuples, not
	// credential ownership links. Do not route these through the credential sync
	// uniqueness guard.
	// TODO(#1823): Add declarative reconciliation for deploy-managed bot grants.
	rel := managedSubjectExternalIdentityRelationship(subjectID, ref)
	if rel == nil {
		return fmt.Errorf("external identity relationship is required")
	}
	modelID, err := s.managedAuthorizationModelID(ctx)
	if err != nil {
		return err
	}
	_ = modelID
	return s.writeAuthorizationRelationships(ctx, []*core.Relationship{rel}, nil)
}

func (s *Server) deleteManagedSubjectExternalIdentityRelationship(ctx context.Context, subjectID string, ref externalIdentityRef) (bool, error) {
	if s.authorizationProvider == nil {
		return false, errAdminAuthorizationUnavailable
	}
	rel := managedSubjectExternalIdentityRelationship(subjectID, ref)
	if rel == nil {
		return false, fmt.Errorf("external identity relationship is required")
	}
	existing, err := s.readAllAuthorizationRelationships(ctx, &core.ListRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Filter: &core.RelationshipFilter{
			Target:   authorization.RelationshipTarget(rel),
			Resource: authorization.RelationshipResource(rel),
		},
	})
	if err != nil {
		return false, err
	}
	found := false
	for _, candidate := range existing {
		if providerRelationshipKey(candidate) == providerRelationshipKey(rel) {
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}
	modelID, err := s.managedAuthorizationModelID(ctx)
	if err != nil {
		return false, err
	}
	_ = modelID
	if err := s.writeAuthorizationRelationships(ctx, nil, relationshipTuples([]*core.Relationship{rel})); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) replaceProviderDynamicMembership(ctx context.Context, resource *core.ResourceRef, subjectID string, role string) ([]*core.Relationship, func(context.Context), error) {
	modelID, err := s.managedAuthorizationModelID(ctx)
	if err != nil {
		return nil, nil, err
	}
	existing, err := s.providerDynamicRelationshipsForSubject(ctx, resource, subjectID)
	if err != nil {
		return nil, nil, err
	}
	writes := providerDynamicMembershipRelationships(resource, subjectID, role)
	deletes := filterRelationshipKeys(existing, writes)
	if len(writes) == 0 && len(deletes) == 0 {
		return existing, func(context.Context) {}, nil
	}
	if len(writes) > 0 {
		if ensuredModelID, err := s.ensureManagedDynamicRole(ctx, resource, role); err != nil {
			return nil, nil, err
		} else if strings.TrimSpace(ensuredModelID) != "" {
			modelID = strings.TrimSpace(ensuredModelID)
		}
	}
	_ = modelID
	if err := s.writeAuthorizationRelationships(ctx, writes, deletes); err != nil {
		return nil, nil, fmt.Errorf("write authorization relationships: %w", err)
	}
	rollbackDeletes := filterRelationshipKeys(writes, existing)
	rollbackWrites := cloneRelationships(existing)
	return existing, func(rollbackCtx context.Context) {
		_ = s.writeAuthorizationRelationships(rollbackCtx, rollbackWrites, rollbackDeletes)
	}, nil
}

func (s *Server) ensureManagedDynamicRole(ctx context.Context, resource *core.ResourceRef, role string) (string, error) {
	switch strings.TrimSpace(resource.GetType()) {
	case authorization.ProviderResourceTypeAppDynamic, authorization.ProviderResourceTypeAdminDynamic:
	default:
		return s.managedAuthorizationModelID(ctx)
	}
	if ensurer, ok := s.authorizer.(authorization.ManagedAuthorizationDynamicRoleEnsurer); ok {
		return ensurer.EnsureManagedDynamicRole(ctx, resource, role)
	}
	return s.managedAuthorizationModelID(ctx)
}

func (s *Server) deleteProviderDynamicMembership(ctx context.Context, resource *core.ResourceRef, subjectID string) ([]*core.Relationship, func(context.Context), error) {
	modelID, err := s.managedAuthorizationModelID(ctx)
	if err != nil {
		return nil, nil, err
	}
	existing, err := s.providerDynamicRelationshipsForSubject(ctx, resource, subjectID)
	if err != nil {
		return nil, nil, err
	}
	deletes := relationshipTuples(existing)
	if len(deletes) == 0 {
		return existing, nil, nil
	}
	_ = modelID
	if err := s.writeAuthorizationRelationships(ctx, nil, deletes); err != nil {
		return nil, nil, fmt.Errorf("delete authorization relationships: %w", err)
	}
	rollbackWrites := cloneRelationships(existing)
	return existing, func(rollbackCtx context.Context) {
		_ = s.writeAuthorizationRelationships(rollbackCtx, rollbackWrites, nil)
	}, nil
}

func (s *Server) managedAuthorizationModelID(ctx context.Context) (string, error) {
	if s.authorizationProvider == nil {
		return "", errAdminAuthorizationUnavailable
	}
	if resolver, ok := s.authorizer.(authorization.ManagedAuthorizationModelResolver); ok {
		return resolver.ManagedModelID(ctx)
	}
	active, err := s.authorizationProvider.GetActiveModelRef(ctx)
	if err != nil {
		return "", err
	}
	if model := active.GetModel(); model != nil && strings.TrimSpace(model.GetId()) != "" {
		return strings.TrimSpace(model.GetId()), nil
	}
	return "", fmt.Errorf("authorization provider has no active model")
}

func (s *Server) providerDynamicRelationshipsForSubject(ctx context.Context, resource *core.ResourceRef, subjectID string) ([]*core.Relationship, error) {
	if s.authorizationProvider == nil {
		return nil, errAdminAuthorizationUnavailable
	}
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ListRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Filter:   &core.RelationshipFilter{Resource: resource},
	})
	if err != nil {
		return nil, err
	}
	subjectID = strings.TrimSpace(subjectID)
	out := make([]*core.Relationship, 0, len(relationships))
	for _, rel := range relationships {
		match, err := s.providerRelationshipMatchesSubject(ctx, rel, subjectID)
		if err != nil {
			return nil, err
		}
		if match {
			out = append(out, rel)
		}
	}
	return out, nil
}

func (s *Server) providerRelationshipMatchesSubject(_ context.Context, rel *core.Relationship, subjectID string) (bool, error) {
	subject := authorization.RelationshipSubject(rel)
	if subject == nil {
		return false, nil
	}
	subjectType := strings.TrimSpace(subject.GetType())
	relationshipSubjectID := strings.TrimSpace(subject.GetId())
	switch subjectType {
	case authorization.ProviderSubjectTypeSubject:
		return subjectID != "" && relationshipSubjectID == subjectID, nil
	default:
		return false, nil
	}
}

func providerDynamicMembershipRelationships(resource *core.ResourceRef, subjectID, role string) []*core.Relationship {
	role = strings.TrimSpace(role)
	subjectID = strings.TrimSpace(subjectID)
	if resource == nil || role == "" || subjectID == "" {
		return nil
	}
	return []*core.Relationship{{
		Tuple: &core.RelationshipTuple{
			Target:   &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_Subject{Subject: &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: subjectID}}},
			Relation: role,
			Resource: cloneResourceRef(resource),
		},
	}}
}

func managedSubjectExternalIdentityRelationship(subjectID string, ref externalIdentityRef) *core.Relationship {
	subjectID = strings.TrimSpace(subjectID)
	ref = normalizeExternalIdentityRef(ref)
	resourceID := externalIdentityResourceID(ref)
	if subjectID == "" || resourceID == "" {
		return nil
	}
	// TODO(#1823): Keep encoded resource ids internal to the authorization
	// provider boundary.
	return &core.Relationship{
		Tuple: &core.RelationshipTuple{
			Target: &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_Subject{Subject: &core.SubjectRef{
				Type: authorization.ProviderSubjectTypeSubject,
				Id:   subjectID,
			}}},
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   resourceID,
			},
		},
	}
}

func relationshipTuples(rels []*core.Relationship) []*core.RelationshipTuple {
	if len(rels) == 0 {
		return nil
	}
	tuples := make([]*core.RelationshipTuple, 0, len(rels))
	for _, rel := range rels {
		if rel == nil || rel.GetTuple() == nil {
			continue
		}
		tuples = append(tuples, cloneRelationshipTuple(rel.GetTuple()))
	}
	return tuples
}

func filterRelationshipKeys(rels []*core.Relationship, keep []*core.Relationship) []*core.RelationshipTuple {
	if len(rels) == 0 {
		return nil
	}
	keepKeys := map[string]struct{}{}
	for _, rel := range keep {
		keepKeys[providerRelationshipKey(rel)] = struct{}{}
	}
	tuples := make([]*core.RelationshipTuple, 0, len(rels))
	for _, rel := range rels {
		if rel == nil {
			continue
		}
		if _, ok := keepKeys[providerRelationshipKey(rel)]; ok {
			continue
		}
		tuples = append(tuples, cloneRelationshipTuple(rel.GetTuple()))
	}
	return tuples
}

func providerRelationshipKey(rel *core.Relationship) string {
	if rel == nil || authorization.RelationshipResource(rel) == nil {
		return ""
	}
	return strings.Join([]string{
		authorization.RelationshipTargetMapKey(authorization.RelationshipTarget(rel), authorization.RelationshipSubject(rel)),
		strings.TrimSpace(authorization.RelationshipRelation(rel)),
		strings.TrimSpace(authorization.RelationshipResource(rel).GetType()),
		strings.TrimSpace(authorization.RelationshipResource(rel).GetId()),
	}, "\x00")
}

func providerRelationshipTupleKey(tuple *core.RelationshipTuple) string {
	if tuple == nil {
		return ""
	}
	return providerRelationshipKey(&core.Relationship{Tuple: tuple})
}

func cloneRelationships(rels []*core.Relationship) []*core.Relationship {
	if len(rels) == 0 {
		return nil
	}
	out := make([]*core.Relationship, 0, len(rels))
	for _, rel := range rels {
		if cloned := cloneRelationship(rel); cloned != nil {
			out = append(out, cloned)
		}
	}
	return out
}

func cloneRelationship(rel *core.Relationship) *core.Relationship {
	if rel == nil {
		return nil
	}
	return &core.Relationship{
		Tuple:       cloneRelationshipTuple(rel.GetTuple()),
		Properties:  rel.GetProperties(),
		SourceLayer: rel.GetSourceLayer(),
	}
}

func (s *Server) writeAuthorizationRelationships(ctx context.Context, writes []*core.Relationship, deletes []*core.RelationshipTuple) error {
	hasChanges := false
	for _, tuple := range deletes {
		if tuple != nil {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		for _, rel := range writes {
			if rel != nil {
				hasChanges = true
				break
			}
		}
	}
	if !hasChanges {
		return nil
	}

	existing, err := s.readAllAuthorizationRelationships(ctx, &core.ListRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
	})
	if err != nil {
		return err
	}
	nextByKey := make(map[string]*core.Relationship, len(existing)+len(writes))
	for _, rel := range existing {
		key := providerRelationshipKey(rel)
		if key == "" {
			continue
		}
		nextByKey[key] = cloneRelationship(rel)
	}
	for _, tuple := range deletes {
		if tuple == nil {
			continue
		}
		if key := providerRelationshipTupleKey(tuple); key != "" {
			delete(nextByKey, key)
		}
	}
	for _, rel := range writes {
		if rel == nil {
			continue
		}
		if key := providerRelationshipKey(rel); key != "" {
			nextByKey[key] = cloneRelationship(rel)
		}
	}
	next := make([]*core.Relationship, 0, len(nextByKey))
	for _, rel := range nextByKey {
		next = append(next, rel)
	}
	sort.Slice(next, func(i, j int) bool {
		return providerRelationshipKey(next[i]) < providerRelationshipKey(next[j])
	})
	if _, err := s.authorizationProvider.SetRelationships(ctx, &core.SetRelationshipsRequest{Relationships: next}); err != nil {
		return err
	}
	return nil
}

func cloneSubjectRef(subject *core.SubjectRef) *core.SubjectRef {
	if subject == nil {
		return nil
	}
	return &core.SubjectRef{
		Type: subject.GetType(),
		Id:   subject.GetId(),
	}
}

func cloneResourceRef(resource *core.ResourceRef) *core.ResourceRef {
	if resource == nil {
		return nil
	}
	return &core.ResourceRef{
		Type: resource.GetType(),
		Id:   resource.GetId(),
	}
}

func cloneRelationshipTuple(tuple *core.RelationshipTuple) *core.RelationshipTuple {
	if tuple == nil {
		return nil
	}
	return &core.RelationshipTuple{
		Target:   cloneRelationshipTarget(tuple.GetTarget(), nil),
		Relation: tuple.GetRelation(),
		Resource: cloneResourceRef(tuple.GetResource()),
	}
}

func cloneRelationshipTarget(target *core.RelationshipTargetRef, subject *core.SubjectRef) *core.RelationshipTargetRef {
	if target != nil {
		if targetSubject := target.GetSubject(); targetSubject != nil {
			return &core.RelationshipTargetRef{
				Kind: &proto.RelationshipTarget_Subject{Subject: cloneSubjectRef(targetSubject)},
			}
		}
		if targetResource := target.GetResource(); targetResource != nil {
			return &core.RelationshipTargetRef{
				Kind: &proto.RelationshipTarget_Resource{Resource: cloneResourceRef(targetResource)},
			}
		}
		if targetSet := target.GetSubjectSet(); targetSet != nil {
			return &core.RelationshipTargetRef{
				Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &core.SubjectSetRef{
					Resource: cloneResourceRef(targetSet.GetResource()),
					Relation: targetSet.GetRelation(),
				}},
			}
		}
	}
	if subject == nil {
		return nil
	}
	return &core.RelationshipTargetRef{
		Kind: &proto.RelationshipTarget_Subject{Subject: cloneSubjectRef(subject)},
	}
}
