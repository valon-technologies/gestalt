package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/authorization"
)

type providerPluginAuthorizationMembership struct {
	App       string
	SubjectID string
	Role      string
}

type providerAdminAuthorizationMembership struct {
	SubjectID string
	Role      string
}

func (s *Server) upsertProviderPluginAuthorization(ctx context.Context, subject *adminAuthorizationWriteSubject, plugin, role string) (*providerPluginAuthorizationMembership, error) {
	if s.authorizationProvider == nil {
		return nil, errAdminAuthorizationUnavailable
	}
	if subject == nil || strings.TrimSpace(subject.SubjectID) == "" {
		return nil, fmt.Errorf("subject is required")
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypeAppDynamic,
		Id:   strings.TrimSpace(plugin),
	}
	_, _, err := s.replaceProviderDynamicMembership(ctx, resource, subject.SubjectID, strings.TrimSpace(role))
	if err != nil {
		return nil, err
	}
	return &providerPluginAuthorizationMembership{
		App:       plugin,
		SubjectID: strings.TrimSpace(subject.SubjectID),
		Role:      role,
	}, nil
}

func (s *Server) deleteProviderPluginAuthorization(ctx context.Context, plugin, subjectID string) error {
	if s.authorizationProvider == nil {
		return errAdminAuthorizationUnavailable
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypeAppDynamic,
		Id:   strings.TrimSpace(plugin),
	}
	existing, _, err := s.deleteProviderDynamicMembership(ctx, resource, subjectID)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *Server) upsertSourceAppAuthorization(ctx context.Context, subject *adminAuthorizationWriteSubject, app, role string) (*providerPluginAuthorizationMembership, error) {
	if s.authorizationProvider == nil {
		return nil, errAdminAuthorizationUnavailable
	}
	if subject == nil || strings.TrimSpace(subject.SubjectID) == "" {
		return nil, fmt.Errorf("subject is required")
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypeAppDynamic,
		Id:   strings.TrimSpace(app),
	}
	if _, _, err := s.replaceProviderDynamicMembership(ctx, resource, subject.SubjectID, strings.TrimSpace(role)); err != nil {
		return nil, err
	}
	return &providerPluginAuthorizationMembership{
		App:       app,
		SubjectID: strings.TrimSpace(subject.SubjectID),
		Role:      strings.TrimSpace(role),
	}, nil
}

func (s *Server) deleteSourceAppAuthorization(ctx context.Context, app, subjectID string) error {
	if s.authorizationProvider == nil {
		return errAdminAuthorizationUnavailable
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypeAppDynamic,
		Id:   strings.TrimSpace(app),
	}
	existing, _, err := s.deleteProviderDynamicMembership(ctx, resource, subjectID)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *Server) upsertSourceAdminAuthorization(ctx context.Context, subject *adminAuthorizationWriteSubject, role string) (*providerAdminAuthorizationMembership, error) {
	if s.authorizationProvider == nil {
		return nil, errAdminAuthorizationUnavailable
	}
	if subject == nil || strings.TrimSpace(subject.SubjectID) == "" {
		return nil, fmt.Errorf("subject is required")
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypeAdminDynamic,
		Id:   authorization.ProviderResourceIDAdminDynamicGlobal,
	}
	if _, _, err := s.replaceProviderDynamicMembership(ctx, resource, subject.SubjectID, strings.TrimSpace(role)); err != nil {
		return nil, err
	}
	return &providerAdminAuthorizationMembership{
		SubjectID: strings.TrimSpace(subject.SubjectID),
		Role:      strings.TrimSpace(role),
	}, nil
}

func (s *Server) deleteSourceAdminAuthorization(ctx context.Context, subjectID string) error {
	if s.authorizationProvider == nil {
		return errAdminAuthorizationUnavailable
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypeAdminDynamic,
		Id:   authorization.ProviderResourceIDAdminDynamicGlobal,
	}
	existing, _, err := s.deleteProviderDynamicMembership(ctx, resource, subjectID)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *Server) replaceProviderDynamicMembership(ctx context.Context, resource *core.ResourceRef, subjectID string, role string) ([]*core.Relationship, func(context.Context), error) {
	if _, err := s.managedAuthorizationModelID(ctx); err != nil {
		return nil, nil, err
	}
	existing, err := s.providerDynamicRelationshipsForSubject(ctx, resource, subjectID)
	if err != nil {
		return nil, nil, err
	}
	desired := providerDynamicMembershipRelationships(resource, subjectID, role)
	deletes := filterRelationshipKeys(existing, desired)
	writes := filterRelationshipKeys(desired, existing)
	if len(writes) == 0 && len(deletes) == 0 {
		return existing, func(context.Context) {}, nil
	}
	if len(writes) > 0 {
		if err := s.ensureManagedDynamicRole(ctx, resource, role); err != nil {
			return nil, nil, err
		}
	}
	if err := s.writeAuthorizationRelationships(ctx, writes, deletes); err != nil {
		return nil, nil, fmt.Errorf("write authorization relationships: %w", err)
	}
	rollbackDeletes := filterRelationshipKeys(writes, existing)
	rollbackWrites := cloneRelationships(existing)
	return existing, func(rollbackCtx context.Context) {
		_ = s.writeAuthorizationRelationships(rollbackCtx, rollbackWrites, rollbackDeletes)
	}, nil
}

func (s *Server) ensureManagedDynamicRole(ctx context.Context, resource *core.ResourceRef, role string) error {
	switch strings.TrimSpace(resource.GetType()) {
	case authorization.ProviderResourceTypeAppDynamic, authorization.ProviderResourceTypeAdminDynamic:
	default:
		_, err := s.managedAuthorizationModelID(ctx)
		return err
	}
	if ensurer, ok := s.authorizer.(authorization.ManagedAuthorizationDynamicRoleEnsurer); ok {
		_, err := ensurer.EnsureManagedDynamicRole(ctx, resource, role)
		return err
	}
	_, err := s.managedAuthorizationModelID(ctx)
	return err
}

func (s *Server) deleteProviderDynamicMembership(ctx context.Context, resource *core.ResourceRef, subjectID string) ([]*core.Relationship, func(context.Context), error) {
	if _, err := s.managedAuthorizationModelID(ctx); err != nil {
		return nil, nil, err
	}
	existing, err := s.providerDynamicRelationshipsForSubject(ctx, resource, subjectID)
	if err != nil {
		return nil, nil, err
	}
	deletes := cloneRelationships(existing)
	if len(deletes) == 0 {
		return existing, nil, nil
	}
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

func filterRelationshipKeys(rels []*core.Relationship, keep []*core.Relationship) []*core.Relationship {
	if len(rels) == 0 {
		return nil
	}
	keepKeys := map[string]struct{}{}
	for _, rel := range keep {
		keepKeys[providerRelationshipKey(rel)] = struct{}{}
	}
	out := make([]*core.Relationship, 0, len(rels))
	for _, rel := range rels {
		if rel == nil {
			continue
		}
		if _, ok := keepKeys[providerRelationshipKey(rel)]; ok {
			continue
		}
		if cloned := cloneRelationship(rel); cloned != nil {
			out = append(out, cloned)
		}
	}
	return out
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

func (s *Server) writeAuthorizationRelationships(ctx context.Context, writes []*core.Relationship, deletes []*core.Relationship) error {
	appliedDeletes := make([]*core.Relationship, 0, len(deletes))
	for _, rel := range deletes {
		cloned := cloneRelationship(rel)
		if cloned == nil || cloned.GetTuple() == nil {
			continue
		}
		if err := s.deleteAuthorizationProviderRelationship(ctx, cloned); err != nil {
			return s.rollbackAuthorizationRelationshipWrite(ctx, err, appliedDeletes, nil)
		}
		appliedDeletes = append(appliedDeletes, cloned)
	}

	appliedWrites := make([]*core.Relationship, 0, len(writes))
	for _, rel := range writes {
		cloned := cloneRelationship(rel)
		if cloned == nil || cloned.GetTuple() == nil {
			continue
		}
		if err := s.addAuthorizationProviderRelationship(ctx, cloned); err != nil {
			return s.rollbackAuthorizationRelationshipWrite(ctx, err, appliedDeletes, appliedWrites)
		}
		appliedWrites = append(appliedWrites, cloned)
	}
	return nil
}

func (s *Server) rollbackAuthorizationRelationshipWrite(ctx context.Context, cause error, appliedDeletes, appliedWrites []*core.Relationship) error {
	if len(appliedDeletes) == 0 && len(appliedWrites) == 0 {
		return cause
	}
	rollbackCtx := context.WithoutCancel(ctx)
	errs := []error{cause}
	for i := len(appliedWrites) - 1; i >= 0; i-- {
		if err := s.deleteAuthorizationProviderRelationship(rollbackCtx, appliedWrites[i]); err != nil {
			errs = append(errs, fmt.Errorf("rollback added authorization relationship: %w", err))
		}
	}
	for i := len(appliedDeletes) - 1; i >= 0; i-- {
		if err := s.addAuthorizationProviderRelationship(rollbackCtx, appliedDeletes[i]); err != nil {
			errs = append(errs, fmt.Errorf("rollback deleted authorization relationship: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *Server) addAuthorizationProviderRelationship(ctx context.Context, rel *core.Relationship) error {
	_, err := s.authorizationProvider.AddRelationship(ctx, &core.AddRelationshipRequest{
		Relationship: cloneRelationship(rel),
	})
	return err
}

func (s *Server) deleteAuthorizationProviderRelationship(ctx context.Context, rel *core.Relationship) error {
	_, err := s.authorizationProvider.DeleteRelationship(ctx, &core.DeleteRelationshipRequest{
		RelationshipTuple: cloneRelationshipTuple(rel.GetTuple()),
		SourceLayer:       rel.GetSourceLayer(),
	})
	return err
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
