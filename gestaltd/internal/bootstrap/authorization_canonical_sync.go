package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

const providerCanonicalSyncReadPageSize = 500

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

	if services.IdentityPluginAccess != nil {
		accesses, err := services.IdentityPluginAccess.ListAll(ctx)
		if err != nil {
			return fmt.Errorf("list canonical plugin access for provider sync: %w", err)
		}
		for _, access := range accesses {
			if access == nil {
				continue
			}
			identityID, err := humanCanonicalIdentityID(ctx, services, access.IdentityID)
			if err != nil {
				return err
			}
			if identityID != "" {
				candidateIdentityIDs[identityID] = struct{}{}
			}
		}
	}

	if services.WorkspaceRoles != nil {
		roles, err := services.WorkspaceRoles.ListAll(ctx)
		if err != nil {
			return fmt.Errorf("list canonical workspace roles for provider sync: %w", err)
		}
		for _, role := range roles {
			if role == nil {
				continue
			}
			identityID, err := humanCanonicalIdentityID(ctx, services, role.IdentityID)
			if err != nil {
				return err
			}
			if identityID != "" {
				candidateIdentityIDs[identityID] = struct{}{}
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
	if resolver, ok := authorizer.(authorization.ManagedAuthorizationModelResolver); ok {
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
	default:
		return "", core.ErrNotFound
	}
}

func humanCanonicalIdentityID(ctx context.Context, services *coredata.Services, identityID string) (string, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" || services == nil || services.Identities == nil {
		return "", nil
	}
	identity, err := services.Identities.GetIdentity(ctx, identityID)
	switch {
	case err == nil:
	case errors.Is(err, core.ErrNotFound):
		return "", nil
	default:
		return "", err
	}
	if identityMetadataLabel(identity.MetadataJSON) != "user" {
		return "", nil
	}
	return identityID, nil
}

func identityMetadataLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var payload struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Label)
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
