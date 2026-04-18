package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

const providerCanonicalSyncReadPageSize = 500

type managedAuthorizationModelResolver interface {
	ManagedModelID(ctx context.Context) (string, error)
}

func syncProviderBackedHumanCanonicalState(ctx context.Context, services *coredata.Services, authorizer authorization.RuntimeAuthorizer, provider core.AuthorizationProvider) error {
	if services == nil || provider == nil || services.Users == nil {
		return nil
	}

	modelID, err := providerBackedModelID(ctx, authorizer, provider)
	if err != nil {
		return err
	}
	if strings.TrimSpace(modelID) == "" {
		return nil
	}

	relationships, err := readAllAuthorizationRelationships(ctx, provider, modelID)
	if err != nil {
		return err
	}

	desiredPluginAccess := map[string]map[string]struct{}{}
	desiredWorkspaceRoles := map[string]map[string]struct{}{}
	candidateIdentityIDs := map[string]struct{}{}

	for _, rel := range relationships {
		if rel == nil || rel.GetSubject() == nil || rel.GetResource() == nil {
			continue
		}
		identityID, err := providerRelationshipIdentityID(ctx, services.Users, rel.GetSubject())
		switch {
		case err == nil:
		case errors.Is(err, core.ErrNotFound):
			continue
		default:
			return err
		}
		if identityID == "" {
			continue
		}
		candidateIdentityIDs[identityID] = struct{}{}

		switch strings.TrimSpace(rel.GetResource().GetType()) {
		case authorization.ProviderResourceTypePluginDynamic:
			plugin := strings.TrimSpace(rel.GetResource().GetId())
			if plugin == "" {
				continue
			}
			ensureStringSet(desiredPluginAccess, identityID)[plugin] = struct{}{}
		case authorization.ProviderResourceTypeAdminDynamic:
			if strings.TrimSpace(rel.GetResource().GetId()) != authorization.ProviderResourceIDAdminDynamicGlobal {
				continue
			}
			role := strings.TrimSpace(rel.GetRelation())
			if role == "" {
				continue
			}
			ensureStringSet(desiredWorkspaceRoles, identityID)[role] = struct{}{}
		}
	}

	if services.PluginAuthorizations != nil {
		memberships, err := services.PluginAuthorizations.ListPluginAuthorizations(ctx)
		if err != nil {
			return fmt.Errorf("list legacy plugin authorizations for canonical sync: %w", err)
		}
		for _, membership := range memberships {
			identityID, err := legacyMembershipIdentityID(ctx, services.Users, membership.UserID, membership.Email)
			switch {
			case err == nil:
				if identityID != "" {
					candidateIdentityIDs[identityID] = struct{}{}
				}
			case errors.Is(err, core.ErrNotFound):
			default:
				return err
			}
		}
	}

	if services.AdminAuthorizations != nil {
		memberships, err := services.AdminAuthorizations.ListAdminAuthorizations(ctx)
		if err != nil {
			return fmt.Errorf("list legacy admin authorizations for canonical sync: %w", err)
		}
		for _, membership := range memberships {
			identityID, err := legacyMembershipIdentityID(ctx, services.Users, membership.UserID, membership.Email)
			switch {
			case err == nil:
				if identityID != "" {
					candidateIdentityIDs[identityID] = struct{}{}
				}
			case errors.Is(err, core.ErrNotFound):
			default:
				return err
			}
		}
	}

	for identityID := range candidateIdentityIDs {
		if err := reconcileProviderPluginCanonicalAccess(ctx, services.IdentityPluginAccess, identityID, desiredPluginAccess[identityID]); err != nil {
			return err
		}
		if err := reconcileProviderWorkspaceRoles(ctx, services.WorkspaceRoles, identityID, desiredWorkspaceRoles[identityID]); err != nil {
			return err
		}
	}

	return nil
}

func providerBackedModelID(ctx context.Context, authorizer authorization.RuntimeAuthorizer, provider core.AuthorizationProvider) (string, error) {
	if resolver, ok := authorizer.(managedAuthorizationModelResolver); ok {
		modelID, err := resolver.ManagedModelID(ctx)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(modelID), nil
	}

	active, err := provider.GetActiveModel(ctx)
	if err != nil {
		return "", fmt.Errorf("get active authorization model: %w", err)
	}
	if model := active.GetModel(); model != nil {
		return strings.TrimSpace(model.GetId()), nil
	}
	return "", nil
}

func readAllAuthorizationRelationships(ctx context.Context, provider core.AuthorizationProvider, modelID string) ([]*core.Relationship, error) {
	pageToken := ""
	var out []*core.Relationship
	for {
		resp, err := provider.ReadRelationships(ctx, &core.ReadRelationshipsRequest{
			PageSize:  providerCanonicalSyncReadPageSize,
			PageToken: pageToken,
			ModelId:   modelID,
		})
		if err != nil {
			return nil, fmt.Errorf("read authorization relationships for canonical sync: %w", err)
		}
		out = append(out, resp.GetRelationships()...)
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return out, nil
		}
	}
}

func providerRelationshipIdentityID(ctx context.Context, users *coredata.UserService, subject *core.SubjectRef) (string, error) {
	if users == nil || subject == nil {
		return "", core.ErrNotFound
	}
	switch strings.TrimSpace(subject.GetType()) {
	case authorization.ProviderSubjectTypeUser:
		return users.CanonicalIdentityIDForUser(ctx, strings.TrimSpace(subject.GetId()))
	case authorization.ProviderSubjectTypeEmail:
		user, err := users.FindUserByEmail(ctx, strings.TrimSpace(subject.GetId()))
		if err != nil {
			return "", err
		}
		return users.CanonicalIdentityIDForUser(ctx, user.ID)
	default:
		return "", core.ErrNotFound
	}
}

func legacyMembershipIdentityID(ctx context.Context, users *coredata.UserService, userID, email string) (string, error) {
	if users == nil {
		return "", core.ErrNotFound
	}
	if userID = strings.TrimSpace(userID); userID != "" {
		return users.CanonicalIdentityIDForUser(ctx, userID)
	}
	if email = strings.TrimSpace(email); email != "" {
		user, err := users.FindUserByEmail(ctx, email)
		if err != nil {
			return "", err
		}
		return users.CanonicalIdentityIDForUser(ctx, user.ID)
	}
	return "", core.ErrNotFound
}

func reconcileProviderPluginCanonicalAccess(ctx context.Context, svc *coredata.IdentityPluginAccessService, identityID string, desired map[string]struct{}) error {
	if svc == nil || strings.TrimSpace(identityID) == "" {
		return nil
	}

	existing, err := svc.ListByIdentity(ctx, identityID)
	if err != nil {
		return err
	}

	for plugin := range desired {
		if _, err := svc.UpsertAccess(ctx, &core.IdentityPluginAccess{
			IdentityID:          identityID,
			Plugin:              plugin,
			InvokeAllOperations: true,
		}); err != nil {
			return fmt.Errorf("sync provider-backed plugin access %q/%q: %w", identityID, plugin, err)
		}
	}

	for _, access := range existing {
		if access == nil {
			continue
		}
		plugin := strings.TrimSpace(access.Plugin)
		if _, ok := desired[plugin]; ok {
			continue
		}
		if err := svc.DeleteAccess(ctx, identityID, plugin); err != nil && !errors.Is(err, core.ErrNotFound) {
			return fmt.Errorf("delete stale provider-backed plugin access %q/%q: %w", identityID, plugin, err)
		}
	}

	return nil
}

func reconcileProviderWorkspaceRoles(ctx context.Context, svc *coredata.WorkspaceRoleService, identityID string, desired map[string]struct{}) error {
	if svc == nil || strings.TrimSpace(identityID) == "" {
		return nil
	}

	existing, err := svc.ListByIdentity(ctx, identityID)
	if err != nil {
		return err
	}

	for role := range desired {
		if _, err := svc.UpsertRole(ctx, &core.WorkspaceRole{
			IdentityID: identityID,
			Role:       role,
		}); err != nil {
			return fmt.Errorf("sync provider-backed workspace role %q/%q: %w", identityID, role, err)
		}
	}

	for _, role := range existing {
		if role == nil {
			continue
		}
		name := strings.TrimSpace(role.Role)
		if _, ok := desired[name]; ok {
			continue
		}
		if err := svc.DeleteRole(ctx, identityID, name); err != nil && !errors.Is(err, core.ErrNotFound) {
			return fmt.Errorf("delete stale provider-backed workspace role %q/%q: %w", identityID, name, err)
		}
	}

	return nil
}

func ensureStringSet(target map[string]map[string]struct{}, key string) map[string]struct{} {
	values := target[key]
	if values == nil {
		values = map[string]struct{}{}
		target[key] = values
	}
	return values
}
