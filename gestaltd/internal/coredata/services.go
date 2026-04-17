package coredata

import (
	"context"
	"fmt"

	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type Services struct {
	Users                          *UserService
	Tokens                         *TokenService
	APITokens                      *APITokenService
	ManagedIdentities              *ManagedIdentityService
	IdentityMemberships            *ManagedIdentityMembershipService
	IdentityGrants                 *ManagedIdentityGrantService
	PluginAuthorizations           *PluginAuthorizationService
	AdminAuthorizations            *AdminAuthorizationService
	Principals                     *PrincipalService
	UserProfiles                   *UserProfileService
	ServiceAccounts                *ServiceAccountService
	ServiceAccountManagementGrants *ServiceAccountManagementGrantService
	WorkspaceRoles                 *WorkspaceRoleService
	PrincipalPluginAccess          *PrincipalPluginAccessService
	ServiceAccountDelegations      *ServiceAccountDelegationService
	APITokenAccess                 *APITokenAccessService
	ExternalCredentials            *ExternalCredentialService
	ServiceAccountAuthBindings     *ServiceAccountAuthBindingService
	DB                             indexeddb.IndexedDB
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
	if err := ds.CreateObjectStore(ctx, StorePluginAuthorizationMemberships, PluginAuthorizationMembershipsSchema); err != nil {
		return nil, fmt.Errorf("create plugin_authorization_memberships store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAdminAuthorizationMemberships, AdminAuthorizationMembershipsSchema); err != nil {
		return nil, fmt.Errorf("create admin_authorization_memberships store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StorePrincipals, PrincipalsSchema); err != nil {
		return nil, fmt.Errorf("create principals store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreUserProfiles, UserProfilesSchema); err != nil {
		return nil, fmt.Errorf("create user_profiles store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreServiceAccounts, ServiceAccountsSchema); err != nil {
		return nil, fmt.Errorf("create service_accounts store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreServiceAccountManagementGrants, ServiceAccountManagementGrantsSchema); err != nil {
		return nil, fmt.Errorf("create service_account_management_grants store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreWorkspaceRoles, WorkspaceRolesSchema); err != nil {
		return nil, fmt.Errorf("create workspace_roles store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StorePrincipalPluginAccess, PrincipalPluginAccessSchema); err != nil {
		return nil, fmt.Errorf("create principal_plugin_access store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreServiceAccountDelegations, ServiceAccountDelegationsSchema); err != nil {
		return nil, fmt.Errorf("create service_account_delegations store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAPITokenAccess, APITokenAccessSchema); err != nil {
		return nil, fmt.Errorf("create api_token_access store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreExternalCredentials, ExternalCredentialsSchema); err != nil {
		return nil, fmt.Errorf("create external_credentials store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreServiceAccountAuthBindings, ServiceAccountAuthBindingsSchema); err != nil {
		return nil, fmt.Errorf("create service_account_auth_bindings store: %w", err)
	}
	principals := NewPrincipalService(ds)
	userProfiles := NewUserProfileService(ds)
	serviceAccounts := NewServiceAccountService(ds)
	serviceAccountManagementGrants := NewServiceAccountManagementGrantService(ds)
	workspaceRoles := NewWorkspaceRoleService(ds)
	principalPluginAccess := NewPrincipalPluginAccessService(ds)
	serviceAccountDelegations := NewServiceAccountDelegationService(ds)
	apiTokenAccess := NewAPITokenAccessService(ds)
	externalCredentials := NewExternalCredentialService(ds)
	serviceAccountAuthBindings := NewServiceAccountAuthBindingService(ds)
	if err := ensureLegacySharedServiceAccount(ctx, principals, serviceAccounts); err != nil {
		return nil, fmt.Errorf("seed legacy shared service account: %w", err)
	}
	users := NewUserService(ds, principals, userProfiles)
	if err := users.BackfillNormalizedEmails(ctx); err != nil {
		return nil, fmt.Errorf("backfill users store: %w", err)
	}
	managedIdentities := NewManagedIdentityService(ds, principals, serviceAccounts)
	identityMemberships := NewManagedIdentityMembershipService(ds, serviceAccountManagementGrants)
	identityGrants := NewManagedIdentityGrantService(ds, principalPluginAccess)
	apiTokens := NewAPITokenService(ds, apiTokenAccess)
	pluginAuthorizations := NewPluginAuthorizationService(ds, nil)
	adminAuthorizations := NewAdminAuthorizationService(ds, workspaceRoles)
	tokens := NewTokenService(ds, enc, externalCredentials)
	if err := users.BackfillCanonicalPrincipals(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical user principals: %w", err)
	}
	if err := managedIdentities.BackfillCanonicalServiceAccounts(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical service accounts: %w", err)
	}
	if err := identityMemberships.BackfillCanonicalGrants(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical service account management grants: %w", err)
	}
	if err := identityGrants.BackfillCanonicalAccess(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical identity grants: %w", err)
	}
	if err := adminAuthorizations.BackfillCanonicalWorkspaceRoles(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical workspace roles: %w", err)
	}
	if err := apiTokens.BackfillTokenAccess(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical api token access: %w", err)
	}
	if err := tokens.BackfillCanonicalCredentials(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical external credentials: %w", err)
	}
	return &Services{
		Users:                          users,
		Tokens:                         tokens,
		APITokens:                      apiTokens,
		ManagedIdentities:              managedIdentities,
		IdentityMemberships:            identityMemberships,
		IdentityGrants:                 identityGrants,
		PluginAuthorizations:           pluginAuthorizations,
		AdminAuthorizations:            adminAuthorizations,
		Principals:                     principals,
		UserProfiles:                   userProfiles,
		ServiceAccounts:                serviceAccounts,
		ServiceAccountManagementGrants: serviceAccountManagementGrants,
		WorkspaceRoles:                 workspaceRoles,
		PrincipalPluginAccess:          principalPluginAccess,
		ServiceAccountDelegations:      serviceAccountDelegations,
		APITokenAccess:                 apiTokenAccess,
		ExternalCredentials:            externalCredentials,
		ServiceAccountAuthBindings:     serviceAccountAuthBindings,
		DB:                             ds,
	}, nil
}

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}
