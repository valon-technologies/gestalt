package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
)

type providerPluginAuthorizationMembership struct {
	Plugin    string
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
		Type: authorization.ProviderResourceTypePluginDynamic,
		Id:   strings.TrimSpace(plugin),
	}
	_, _, err := s.replaceProviderDynamicMembership(ctx, resource, subject.SubjectID, strings.TrimSpace(role))
	if err != nil {
		return nil, err
	}
	return &providerPluginAuthorizationMembership{
		Plugin:    plugin,
		SubjectID: strings.TrimSpace(subject.SubjectID),
		Role:      role,
	}, nil
}

func (s *Server) deleteProviderPluginAuthorization(ctx context.Context, plugin, subjectID string) error {
	if s.authorizationProvider == nil {
		return errAdminAuthorizationUnavailable
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypePluginDynamic,
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

func (s *Server) upsertProviderAdminAuthorization(ctx context.Context, subject *adminAuthorizationWriteSubject, role string) (*providerAdminAuthorizationMembership, error) {
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
	_, _, err := s.replaceProviderDynamicMembership(ctx, resource, subject.SubjectID, strings.TrimSpace(role))
	if err != nil {
		return nil, err
	}
	return &providerAdminAuthorizationMembership{
		SubjectID: strings.TrimSpace(subject.SubjectID),
		Role:      role,
	}, nil
}

func (s *Server) deleteProviderAdminAuthorization(ctx context.Context, subjectID string) error {
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
	if err := s.authorizationProvider.WriteRelationships(ctx, &core.WriteRelationshipsRequest{
		Writes:  writes,
		Deletes: deletes,
		ModelId: modelID,
	}); err != nil {
		return nil, nil, fmt.Errorf("write authorization relationships: %w", err)
	}
	rollbackDeletes := filterRelationshipKeys(writes, existing)
	rollbackWrites := cloneRelationships(existing)
	return existing, func(rollbackCtx context.Context) {
		_ = s.authorizationProvider.WriteRelationships(rollbackCtx, &core.WriteRelationshipsRequest{
			Writes:  rollbackWrites,
			Deletes: rollbackDeletes,
			ModelId: modelID,
		})
	}, nil
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
	deletes := relationshipKeys(existing)
	if len(deletes) == 0 {
		return existing, nil, nil
	}
	if err := s.authorizationProvider.WriteRelationships(ctx, &core.WriteRelationshipsRequest{
		Deletes: deletes,
		ModelId: modelID,
	}); err != nil {
		return nil, nil, fmt.Errorf("delete authorization relationships: %w", err)
	}
	rollbackWrites := cloneRelationships(existing)
	return existing, func(rollbackCtx context.Context) {
		_ = s.authorizationProvider.WriteRelationships(rollbackCtx, &core.WriteRelationshipsRequest{
			Writes:  rollbackWrites,
			ModelId: modelID,
		})
	}, nil
}

func (s *Server) managedAuthorizationModelID(ctx context.Context) (string, error) {
	if s.authorizationProvider == nil {
		return "", errAdminAuthorizationUnavailable
	}
	if resolver, ok := s.authorizer.(authorization.ManagedAuthorizationModelResolver); ok {
		return resolver.ManagedModelID(ctx)
	}
	active, err := s.authorizationProvider.GetActiveModel(ctx)
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
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Resource: resource,
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
	if rel == nil || rel.GetSubject() == nil {
		return false, nil
	}
	subjectType := strings.TrimSpace(rel.GetSubject().GetType())
	relationshipSubjectID := strings.TrimSpace(rel.GetSubject().GetId())
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
		Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: subjectID},
		Relation: role,
		Resource: cloneResourceRef(resource),
	}}
}

func relationshipKeys(rels []*core.Relationship) []*core.RelationshipKey {
	if len(rels) == 0 {
		return nil
	}
	keys := make([]*core.RelationshipKey, 0, len(rels))
	for _, rel := range rels {
		if rel == nil {
			continue
		}
		keys = append(keys, &core.RelationshipKey{
			Subject:  cloneSubjectRef(rel.GetSubject()),
			Relation: rel.GetRelation(),
			Resource: cloneResourceRef(rel.GetResource()),
		})
	}
	return keys
}

func filterRelationshipKeys(rels []*core.Relationship, keep []*core.Relationship) []*core.RelationshipKey {
	if len(rels) == 0 {
		return nil
	}
	keepKeys := map[string]struct{}{}
	for _, rel := range keep {
		keepKeys[providerRelationshipKey(rel)] = struct{}{}
	}
	keys := make([]*core.RelationshipKey, 0, len(rels))
	for _, rel := range rels {
		if rel == nil {
			continue
		}
		if _, ok := keepKeys[providerRelationshipKey(rel)]; ok {
			continue
		}
		keys = append(keys, &core.RelationshipKey{
			Subject:  cloneSubjectRef(rel.GetSubject()),
			Relation: rel.GetRelation(),
			Resource: cloneResourceRef(rel.GetResource()),
		})
	}
	return keys
}

func providerRelationshipKey(rel *core.Relationship) string {
	if rel == nil || rel.GetSubject() == nil || rel.GetResource() == nil {
		return ""
	}
	return strings.Join([]string{
		strings.TrimSpace(rel.GetSubject().GetType()),
		strings.TrimSpace(rel.GetSubject().GetId()),
		strings.TrimSpace(rel.GetRelation()),
		strings.TrimSpace(rel.GetResource().GetType()),
		strings.TrimSpace(rel.GetResource().GetId()),
	}, "\x00")
}

func cloneRelationships(rels []*core.Relationship) []*core.Relationship {
	if len(rels) == 0 {
		return nil
	}
	out := make([]*core.Relationship, 0, len(rels))
	for _, rel := range rels {
		if rel == nil {
			continue
		}
		out = append(out, &core.Relationship{
			Subject:  cloneSubjectRef(rel.GetSubject()),
			Relation: rel.GetRelation(),
			Resource: cloneResourceRef(rel.GetResource()),
		})
	}
	return out
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
