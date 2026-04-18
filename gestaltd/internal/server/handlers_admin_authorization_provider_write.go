package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
)

type managedAuthorizationModelResolver interface {
	ManagedModelID(ctx context.Context) (string, error)
}

func (s *Server) upsertProviderPluginAuthorization(ctx context.Context, user *core.User, plugin, role string) (*coredata.PluginAuthorizationMembership, error) {
	if s.authorizationProvider == nil {
		return nil, errAdminAuthorizationUnavailable
	}
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypePluginDynamic,
		Id:   strings.TrimSpace(plugin),
	}
	_, rollback, err := s.replaceProviderDynamicMembership(ctx, resource, user, strings.TrimSpace(role))
	if err != nil {
		return nil, err
	}
	membership := &coredata.PluginAuthorizationMembership{
		Plugin: plugin,
		UserID: user.ID,
		Email:  user.Email,
		Role:   role,
	}
	if err := s.syncProviderPluginCanonicalAccess(ctx, membership.UserID, membership.Plugin); err != nil {
		rollback(ctx)
		return nil, err
	}
	return membership, nil
}

func (s *Server) deleteProviderPluginAuthorization(ctx context.Context, plugin, userID string) error {
	if s.authorizationProvider == nil {
		return errAdminAuthorizationUnavailable
	}
	user, userErr := s.users.GetUser(ctx, strings.TrimSpace(userID))
	if userErr != nil && !errors.Is(userErr, core.ErrNotFound) {
		return userErr
	}
	subjectUser := &core.User{ID: strings.TrimSpace(userID)}
	if userErr == nil {
		subjectUser = user
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypePluginDynamic,
		Id:   strings.TrimSpace(plugin),
	}
	existing, rollback, err := s.deleteProviderDynamicMembership(ctx, resource, subjectUser)
	if err != nil {
		return err
	}
	err = s.deleteProviderPluginCanonicalAccess(ctx, strings.TrimSpace(userID), strings.TrimSpace(plugin))
	switch {
	case err == nil:
		if len(existing) == 0 {
			return core.ErrNotFound
		}
		return nil
	case errors.Is(err, core.ErrNotFound):
		if len(existing) == 0 {
			return core.ErrNotFound
		}
		return nil
	default:
		if rollback != nil {
			rollback(ctx)
		}
		return err
	}
}

func (s *Server) upsertProviderAdminAuthorization(ctx context.Context, user *core.User, role string) (*coredata.AdminAuthorizationMembership, error) {
	if s.authorizationProvider == nil {
		return nil, errAdminAuthorizationUnavailable
	}
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypeAdminDynamic,
		Id:   authorization.ProviderResourceIDAdminDynamicGlobal,
	}
	existing, rollback, err := s.replaceProviderDynamicMembership(ctx, resource, user, strings.TrimSpace(role))
	if err != nil {
		return nil, err
	}
	membership := &coredata.AdminAuthorizationMembership{
		UserID: user.ID,
		Email:  user.Email,
		Role:   role,
	}
	if err := s.syncProviderAdminCanonicalRole(ctx, membership.UserID, membership.Role, providerRelationshipRelations(existing)); err != nil {
		rollback(ctx)
		return nil, err
	}
	return membership, nil
}

func (s *Server) deleteProviderAdminAuthorization(ctx context.Context, userID string) error {
	if s.authorizationProvider == nil {
		return errAdminAuthorizationUnavailable
	}
	user, userErr := s.users.GetUser(ctx, strings.TrimSpace(userID))
	if userErr != nil && !errors.Is(userErr, core.ErrNotFound) {
		return userErr
	}
	subjectUser := &core.User{ID: strings.TrimSpace(userID)}
	if userErr == nil {
		subjectUser = user
	}
	resource := &core.ResourceRef{
		Type: authorization.ProviderResourceTypeAdminDynamic,
		Id:   authorization.ProviderResourceIDAdminDynamicGlobal,
	}
	existing, rollback, err := s.deleteProviderDynamicMembership(ctx, resource, subjectUser)
	if err != nil {
		return err
	}
	err = s.deleteProviderAdminCanonicalRoles(ctx, strings.TrimSpace(userID), providerRelationshipRelations(existing))
	switch {
	case err == nil:
		if len(existing) == 0 {
			return core.ErrNotFound
		}
		return nil
	case errors.Is(err, core.ErrNotFound):
		if len(existing) == 0 {
			return core.ErrNotFound
		}
		return nil
	default:
		if rollback != nil {
			rollback(ctx)
		}
		return err
	}
}

func (s *Server) replaceProviderDynamicMembership(ctx context.Context, resource *core.ResourceRef, user *core.User, role string) ([]*core.Relationship, func(context.Context), error) {
	modelID, err := s.managedAuthorizationModelID(ctx)
	if err != nil {
		return nil, nil, err
	}
	existing, err := s.providerDynamicRelationshipsForUser(ctx, resource, user)
	if err != nil {
		return nil, nil, err
	}
	writes := providerDynamicMembershipRelationships(resource, user, role)
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

func (s *Server) deleteProviderDynamicMembership(ctx context.Context, resource *core.ResourceRef, user *core.User) ([]*core.Relationship, func(context.Context), error) {
	modelID, err := s.managedAuthorizationModelID(ctx)
	if err != nil {
		return nil, nil, err
	}
	existing, err := s.providerDynamicRelationshipsForUser(ctx, resource, user)
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
	if resolver, ok := s.authorizer.(managedAuthorizationModelResolver); ok {
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

func (s *Server) providerDynamicRelationshipsForUser(ctx context.Context, resource *core.ResourceRef, user *core.User) ([]*core.Relationship, error) {
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
	userID := ""
	email := ""
	if user != nil {
		userID = strings.TrimSpace(user.ID)
		email = emailutil.Normalize(user.Email)
	}
	out := make([]*core.Relationship, 0, len(relationships))
	for _, rel := range relationships {
		match, err := s.providerRelationshipMatchesUser(ctx, rel, userID, email)
		if err != nil {
			return nil, err
		}
		if match {
			out = append(out, rel)
		}
	}
	return out, nil
}

func (s *Server) providerRelationshipMatchesUser(ctx context.Context, rel *core.Relationship, userID, email string) (bool, error) {
	if rel == nil || rel.GetSubject() == nil {
		return false, nil
	}
	subjectType := strings.TrimSpace(rel.GetSubject().GetType())
	subjectID := strings.TrimSpace(rel.GetSubject().GetId())
	switch subjectType {
	case authorization.ProviderSubjectTypeUser:
		return userID != "" && subjectID == userID, nil
	case authorization.ProviderSubjectTypeEmail:
		normalized := emailutil.Normalize(subjectID)
		if normalized == "" {
			return false, nil
		}
		if email != "" && normalized == email {
			return true, nil
		}
		if userID == "" || s.users == nil {
			return false, nil
		}
		user, err := s.users.FindUserByEmail(ctx, normalized)
		switch {
		case err == nil:
			return strings.TrimSpace(user.ID) == userID, nil
		case errors.Is(err, core.ErrNotFound):
			return false, nil
		default:
			return false, err
		}
	default:
		return false, nil
	}
}

func providerDynamicMembershipRelationships(resource *core.ResourceRef, user *core.User, role string) []*core.Relationship {
	role = strings.TrimSpace(role)
	if resource == nil || role == "" || user == nil {
		return nil
	}
	writes := make([]*core.Relationship, 0, 2)
	if userID := strings.TrimSpace(user.ID); userID != "" {
		writes = append(writes, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeUser, Id: userID},
			Relation: role,
			Resource: cloneResourceRef(resource),
		})
	}
	if email := emailutil.Normalize(user.Email); email != "" {
		writes = append(writes, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeEmail, Id: email},
			Relation: role,
			Resource: cloneResourceRef(resource),
		})
	}
	return writes
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

func (s *Server) syncProviderPluginCanonicalAccess(ctx context.Context, userID, plugin string) error {
	if s.identityPluginAccess == nil {
		return nil
	}
	identityID, err := s.canonicalIdentityIDForUser(ctx, userID)
	if err != nil {
		return err
	}
	_, err = s.identityPluginAccess.UpsertAccess(ctx, &core.IdentityPluginAccess{
		IdentityID:          identityID,
		Plugin:              plugin,
		InvokeAllOperations: true,
	})
	if err != nil {
		return fmt.Errorf("sync canonical identity plugin access %q/%q: %w", identityID, plugin, err)
	}
	return nil
}

func (s *Server) deleteProviderPluginCanonicalAccess(ctx context.Context, userID, plugin string) error {
	if s.identityPluginAccess == nil {
		return nil
	}
	identityID, err := s.canonicalIdentityIDForUser(ctx, userID)
	switch {
	case err == nil:
	case errors.Is(err, core.ErrNotFound):
		return nil
	default:
		return err
	}
	if err := s.identityPluginAccess.DeleteAccess(ctx, identityID, plugin); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return core.ErrNotFound
		}
		return fmt.Errorf("delete canonical identity plugin access %q/%q: %w", identityID, plugin, err)
	}
	return nil
}

func (s *Server) syncProviderAdminCanonicalRole(ctx context.Context, userID, role string, staleRoles []string) error {
	if s.workspaceRoles == nil {
		return nil
	}
	identityID, err := s.canonicalIdentityIDForUser(ctx, userID)
	if err != nil {
		return err
	}
	if _, err := s.workspaceRoles.UpsertRole(ctx, &core.WorkspaceRole{
		IdentityID: identityID,
		Role:       strings.TrimSpace(role),
	}); err != nil {
		return fmt.Errorf("sync canonical workspace role %q/%q: %w", identityID, role, err)
	}
	for _, staleRole := range staleRoles {
		staleRole = strings.TrimSpace(staleRole)
		if staleRole == "" || staleRole == strings.TrimSpace(role) {
			continue
		}
		if err := s.workspaceRoles.DeleteRole(ctx, identityID, staleRole); err != nil && !errors.Is(err, core.ErrNotFound) {
			return fmt.Errorf("delete stale canonical workspace role %q/%q: %w", identityID, staleRole, err)
		}
	}
	return nil
}

func (s *Server) deleteProviderAdminCanonicalRoles(ctx context.Context, userID string, roles []string) error {
	if s.workspaceRoles == nil {
		return nil
	}
	identityID, err := s.canonicalIdentityIDForUser(ctx, userID)
	switch {
	case err == nil:
	case errors.Is(err, core.ErrNotFound):
		return nil
	default:
		return err
	}
	deleted := false
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if err := s.workspaceRoles.DeleteRole(ctx, identityID, role); err != nil {
			if errors.Is(err, core.ErrNotFound) {
				continue
			}
			return fmt.Errorf("delete canonical workspace role %q/%q: %w", identityID, role, err)
		}
		deleted = true
	}
	if !deleted && len(roles) > 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *Server) canonicalIdentityIDForUser(ctx context.Context, userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("user id is required")
	}
	if s.users == nil {
		return userID, nil
	}
	return s.users.CanonicalIdentityIDForUser(ctx, userID)
}

func providerRelationshipRelations(rels []*core.Relationship) []string {
	if len(rels) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(rels))
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		if rel == nil {
			continue
		}
		relation := strings.TrimSpace(rel.GetRelation())
		if relation == "" {
			continue
		}
		if _, ok := seen[relation]; ok {
			continue
		}
		seen[relation] = struct{}{}
		out = append(out, relation)
	}
	return out
}
