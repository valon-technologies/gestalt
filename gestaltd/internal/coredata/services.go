package coredata

import (
	"context"
	"fmt"

	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type Services struct {
	Users                    *UserService
	Tokens                   *TokenService
	APITokens                *APITokenService
	ManagedIdentities        *ManagedIdentityService
	IdentityMemberships      *ManagedIdentityMembershipService
	IdentityGrants           *ManagedIdentityGrantService
	Identities               *IdentityService
	IdentityAuthBindings     *IdentityAuthBindingService
	IdentityManagementGrants *IdentityManagementGrantService
	IdentityDelegations      *IdentityDelegationService
	WorkspaceRoles           *WorkspaceRoleService
	IdentityPluginAccess     *IdentityPluginAccessService
	APITokenAccess           *APITokenAccessService
	ExternalCredentials      *ExternalCredentialService
	WorkflowExecutionRefs    *WorkflowExecutionRefService
	DB                       indexeddb.IndexedDB
}

func New(ds indexeddb.IndexedDB, enc *corecrypto.AESGCMEncryptor) (*Services, error) {
	ctx := context.Background()
	if err := ds.CreateObjectStore(ctx, StoreUsers, UsersSchema); err != nil {
		return nil, fmt.Errorf("create users store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreIntegrationTokens, IntegrationTokensSchema); err != nil {
		return nil, fmt.Errorf("create integration_tokens store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAPITokens, APITokensSchema); err != nil {
		return nil, fmt.Errorf("create api_tokens store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreManagedIdentities, ManagedIdentitiesSchema); err != nil {
		return nil, fmt.Errorf("create managed_identities store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreManagedIdentityMemberships, ManagedIdentityMembershipsSchema); err != nil {
		return nil, fmt.Errorf("create managed_identity_memberships store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreManagedIdentityGrants, ManagedIdentityGrantsSchema); err != nil {
		return nil, fmt.Errorf("create managed_identity_grants store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreIdentities, IdentitiesSchema); err != nil {
		return nil, fmt.Errorf("create identities store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreIdentityAuthBindings, IdentityAuthBindingsSchema); err != nil {
		return nil, fmt.Errorf("create identity_auth_bindings store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreIdentityManagementGrants, IdentityManagementGrantsSchema); err != nil {
		return nil, fmt.Errorf("create identity_management_grants store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreIdentityDelegations, IdentityDelegationsSchema); err != nil {
		return nil, fmt.Errorf("create identity_delegations store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreWorkspaceRoles, WorkspaceRolesSchema); err != nil {
		return nil, fmt.Errorf("create workspace_roles store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreIdentityPluginAccess, IdentityPluginAccessSchema); err != nil {
		return nil, fmt.Errorf("create identity_plugin_access store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAPITokenAccess, APITokenAccessSchema); err != nil {
		return nil, fmt.Errorf("create api_token_access store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreExternalCredentials, ExternalCredentialsSchema); err != nil {
		return nil, fmt.Errorf("create external_credentials store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreWorkflowExecutionRefs, WorkflowExecutionRefsSchema); err != nil {
		return nil, fmt.Errorf("create workflow_execution_refs store: %w", err)
	}

	identities := NewIdentityService(ds)
	authBindings := NewIdentityAuthBindingService(ds)
	identityManagementGrants := NewIdentityManagementGrantService(ds)
	identityDelegations := NewIdentityDelegationService(ds)
	workspaceRoles := NewWorkspaceRoleService(ds)
	identityPluginAccess := NewIdentityPluginAccessService(ds)
	apiTokenAccess := NewAPITokenAccessService(ds)
	externalCredentials := NewExternalCredentialService(ds)
	workflowExecutionRefs := NewWorkflowExecutionRefService(ds)

	users := NewUserService(ds, identities, authBindings)
	if err := users.BackfillNormalizedEmails(ctx); err != nil {
		return nil, fmt.Errorf("backfill users store: %w", err)
	}
	managedIdentities := NewManagedIdentityService(ds, identities)
	identityMemberships := NewManagedIdentityMembershipService(ds, identityManagementGrants, users)
	identityGrants := NewManagedIdentityGrantService(ds, identityPluginAccess)
	apiTokens := NewAPITokenService(ds, apiTokenAccess, users)
	tokens := NewTokenService(ds, enc, externalCredentials, users)

	if err := rebuildCanonicalIdentityGraph(ctx, identities, authBindings, identityManagementGrants, workspaceRoles, identityPluginAccess, apiTokenAccess, externalCredentials); err != nil {
		return nil, err
	}
	if err := users.BackfillCanonicalIdentities(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical identities from users: %w", err)
	}
	if err := managedIdentities.BackfillCanonicalIdentities(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical identities from managed identities: %w", err)
	}
	if err := identityMemberships.BackfillCanonicalGrants(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical identity management grants: %w", err)
	}
	if err := identityGrants.BackfillCanonicalAccess(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical identity grants: %w", err)
	}
	if err := apiTokens.BackfillTokenAccess(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical api token access: %w", err)
	}
	if err := tokens.BackfillSubjectIDs(ctx); err != nil {
		return nil, fmt.Errorf("backfill integration token subjects: %w", err)
	}
	if err := tokens.BackfillCanonicalCredentials(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical external credentials: %w", err)
	}

	return &Services{
		Users:                    users,
		Tokens:                   tokens,
		APITokens:                apiTokens,
		ManagedIdentities:        managedIdentities,
		IdentityMemberships:      identityMemberships,
		IdentityGrants:           identityGrants,
		Identities:               identities,
		IdentityAuthBindings:     authBindings,
		IdentityManagementGrants: identityManagementGrants,
		IdentityDelegations:      identityDelegations,
		WorkspaceRoles:           workspaceRoles,
		IdentityPluginAccess:     identityPluginAccess,
		APITokenAccess:           apiTokenAccess,
		ExternalCredentials:      externalCredentials,
		WorkflowExecutionRefs:    workflowExecutionRefs,
		DB:                       ds,
	}, nil
}

func rebuildCanonicalIdentityGraph(ctx context.Context, identities *IdentityService, authBindings *IdentityAuthBindingService, managementGrants *IdentityManagementGrantService, workspaceRoles *WorkspaceRoleService, pluginAccess *IdentityPluginAccessService, apiTokenAccess *APITokenAccessService, externalCredentials *ExternalCredentialService) error {
	for _, store := range []indexeddb.ObjectStore{
		identities.store,
		authBindings.store,
		managementGrants.store,
		workspaceRoles.store,
		pluginAccess.store,
		apiTokenAccess.store,
		externalCredentials.store,
	} {
		if err := store.Clear(ctx); err != nil {
			return fmt.Errorf("clear canonical identity graph store: %w", err)
		}
	}
	return nil
}

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}
