package coredata

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type Services struct {
	Users                    *UserService
	ExternalCredentials      core.ExternalCredentialProvider
	APITokens                *APITokenService
	Identities               *IdentityService
	IdentityAuthBindings     *IdentityAuthBindingService
	IdentityManagementGrants *IdentityManagementGrantService
	IdentityDelegations      *IdentityDelegationService
	WorkspaceRoles           *WorkspaceRoleService
	IdentityPluginAccess     *IdentityPluginAccessService
	APITokenAccess           *APITokenAccessService
	AgentSessions            *AgentSessionMetadataService
	AgentRunMetadata         *AgentRunMetadataService
	RuntimeSessionLogs       *RuntimeSessionLogService
	DB                       indexeddb.IndexedDB
}

func New(ds indexeddb.IndexedDB) (*Services, error) {
	ctx := context.Background()
	if err := ds.CreateObjectStore(ctx, StoreUsers, UsersSchema); err != nil {
		return nil, fmt.Errorf("create users store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAPITokens, APITokensSchema); err != nil {
		return nil, fmt.Errorf("create api_tokens store: %w", err)
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
	if err := ds.CreateObjectStore(ctx, StoreAgentSessionMetadata, AgentSessionMetadataSchema); err != nil {
		return nil, fmt.Errorf("create agent_session_metadata store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAgentSessionIdempotency, AgentSessionIdempotencySchema); err != nil {
		return nil, fmt.Errorf("create agent_session_idempotency store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAgentRunMetadata, AgentRunMetadataSchema); err != nil {
		return nil, fmt.Errorf("create agent_run_metadata store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAgentRunIdempotency, AgentRunIdempotencySchema); err != nil {
		return nil, fmt.Errorf("create agent_run_idempotency store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreRuntimeSessions, RuntimeSessionsSchema); err != nil {
		return nil, fmt.Errorf("create runtime_sessions store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreRuntimeSessionLogs, RuntimeSessionLogsSchema); err != nil {
		return nil, fmt.Errorf("create runtime_session_logs store: %w", err)
	}

	identities := NewIdentityService(ds)
	authBindings := NewIdentityAuthBindingService(ds)
	identityManagementGrants := NewIdentityManagementGrantService(ds)
	identityDelegations := NewIdentityDelegationService(ds)
	workspaceRoles := NewWorkspaceRoleService(ds)
	identityPluginAccess := NewIdentityPluginAccessService(ds)
	apiTokenAccess := NewAPITokenAccessService(ds)
	agentSessions := NewAgentSessionMetadataService(ds)
	agentRunMetadata := NewAgentRunMetadataService(ds)
	runtimeSessionLogs := NewRuntimeSessionLogService(ds)

	users := NewUserService(ds, identities, authBindings)
	if err := users.BackfillNormalizedEmails(ctx); err != nil {
		return nil, fmt.Errorf("backfill users store: %w", err)
	}
	apiTokens := NewAPITokenService(ds, apiTokenAccess, users)

	if err := rebuildCanonicalIdentityGraph(ctx, identities, authBindings, identityManagementGrants, workspaceRoles, identityPluginAccess, apiTokenAccess); err != nil {
		return nil, err
	}
	if err := users.BackfillCanonicalIdentities(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical identities from users: %w", err)
	}
	if err := apiTokens.BackfillTokenAccess(ctx); err != nil {
		return nil, fmt.Errorf("backfill canonical api token access: %w", err)
	}
	return &Services{
		ExternalCredentials:      nil,
		Users:                    users,
		APITokens:                apiTokens,
		Identities:               identities,
		IdentityAuthBindings:     authBindings,
		IdentityManagementGrants: identityManagementGrants,
		IdentityDelegations:      identityDelegations,
		WorkspaceRoles:           workspaceRoles,
		IdentityPluginAccess:     identityPluginAccess,
		APITokenAccess:           apiTokenAccess,
		AgentSessions:            agentSessions,
		AgentRunMetadata:         agentRunMetadata,
		RuntimeSessionLogs:       runtimeSessionLogs,
		DB:                       ds,
	}, nil
}

func rebuildCanonicalIdentityGraph(ctx context.Context, identities *IdentityService, authBindings *IdentityAuthBindingService, managementGrants *IdentityManagementGrantService, workspaceRoles *WorkspaceRoleService, pluginAccess *IdentityPluginAccessService, apiTokenAccess *APITokenAccessService) error {
	for _, store := range []indexeddb.ObjectStore{
		identities.store,
		authBindings.store,
		managementGrants.store,
		workspaceRoles.store,
		pluginAccess.store,
		apiTokenAccess.store,
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
