package coredata

import (
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/externalcredentials"
)

const (
	StoreUsers = "users"
	// StoreIntegrationTokens is kept only as a legacy table name for
	// deployment-managed migrations. New runtime writes use StoreExternalCredentials.
	StoreIntegrationTokens        = "integration_tokens"
	StoreExternalCredentials      = externalcredentials.StoreName
	StoreAPITokens                = "api_tokens"
	StoreIdentities               = "identities"
	StoreIdentityAuthBindings     = "identity_auth_bindings"
	StoreIdentityManagementGrants = "identity_management_grants"
	StoreIdentityDelegations      = "identity_delegations"
	StoreWorkspaceRoles           = "workspace_roles"
	StoreIdentityPluginAccess     = "identity_plugin_access"
	StoreAPITokenAccess           = "api_token_access"
	StoreAgentRunMetadata         = "agent_run_metadata"
	StoreAgentRunIdempotency      = "agent_run_idempotency"
	StoreAgentRunEvents           = "agent_run_events"
)

var UsersSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_email", KeyPath: []string{"email"}, Unique: true},
		{Name: "by_normalized_email", KeyPath: []string{"normalized_email"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "email", Type: indexeddb.TypeString, NotNull: true, Unique: true},
		{Name: "normalized_email", Type: indexeddb.TypeString},
		{Name: "display_name", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var ExternalCredentialsSchema = externalcredentials.Schema

// IntegrationTokensSchema is kept only as a legacy schema alias for
// deployment-managed migrations. New runtime writes use ExternalCredentialsSchema.
var IntegrationTokensSchema = externalcredentials.Schema

var APITokensSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_hash", KeyPath: []string{"hashed_token"}, Unique: true},
		{Name: "by_identity", KeyPath: []string{"identity_id"}},
		{Name: "by_identity_id", KeyPath: []string{"id", "identity_id"}, Unique: true},
		{Name: "by_token_kind", KeyPath: []string{"token_kind"}},
		{Name: "by_owner", KeyPath: []string{"owner_kind", "owner_id"}},
		{Name: "by_owner_id", KeyPath: []string{"id", "owner_kind", "owner_id"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "identity_id", Type: indexeddb.TypeString},
		{Name: "owner_kind", Type: indexeddb.TypeString},
		{Name: "owner_id", Type: indexeddb.TypeString},
		{Name: "token_kind", Type: indexeddb.TypeString},
		{Name: "credential_subject_id", Type: indexeddb.TypeString},
		{Name: "name", Type: indexeddb.TypeString},
		{Name: "hashed_token", Type: indexeddb.TypeString, NotNull: true, Unique: true},
		{Name: "scopes", Type: indexeddb.TypeString},
		{Name: "permissions_json", Type: indexeddb.TypeString},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var IdentitiesSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_status", KeyPath: []string{"status"}},
		{Name: "by_creator", KeyPath: []string{"created_by_identity_id"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "status", Type: indexeddb.TypeString, NotNull: true},
		{Name: "display_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_by_identity_id", Type: indexeddb.TypeString},
		{Name: "metadata_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var IdentityAuthBindingsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_identity", KeyPath: []string{"identity_id"}},
		{Name: "by_binding_kind", KeyPath: []string{"binding_kind"}},
		{Name: "by_lookup", KeyPath: []string{"binding_kind", "authority", "lookup_key"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "identity_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "binding_kind", Type: indexeddb.TypeString, NotNull: true},
		{Name: "authority", Type: indexeddb.TypeString, NotNull: true},
		{Name: "lookup_key", Type: indexeddb.TypeString, NotNull: true},
		{Name: "binding_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var IdentityManagementGrantsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_manager", KeyPath: []string{"manager_identity_id"}},
		{Name: "by_target", KeyPath: []string{"target_identity_id"}},
		{Name: "by_manager_target", KeyPath: []string{"manager_identity_id", "target_identity_id"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "manager_identity_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "target_identity_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "role", Type: indexeddb.TypeString, NotNull: true},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var IdentityDelegationsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_actor", KeyPath: []string{"actor_identity_id"}},
		{Name: "by_target", KeyPath: []string{"target_identity_id"}},
		{Name: "by_actor_target_plugin", KeyPath: []string{"actor_identity_id", "target_identity_id", "plugin"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "actor_identity_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "target_identity_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "plugin", Type: indexeddb.TypeString},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var WorkspaceRolesSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_identity", KeyPath: []string{"identity_id"}},
		{Name: "by_identity_role", KeyPath: []string{"identity_id", "role"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "identity_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "role", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var IdentityPluginAccessSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_identity", KeyPath: []string{"identity_id"}},
		{Name: "by_plugin", KeyPath: []string{"plugin"}},
		{Name: "by_identity_plugin", KeyPath: []string{"identity_id", "plugin"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "identity_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "plugin", Type: indexeddb.TypeString, NotNull: true},
		{Name: "invoke_all_operations", Type: indexeddb.TypeBool, NotNull: true},
		{Name: "operations_json", Type: indexeddb.TypeString},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var APITokenAccessSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_token", KeyPath: []string{"token_id"}},
		{Name: "by_token_plugin", KeyPath: []string{"token_id", "plugin"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "token_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "plugin", Type: indexeddb.TypeString, NotNull: true},
		{Name: "invoke_all_operations", Type: indexeddb.TypeBool, NotNull: true},
		{Name: "operations_json", Type: indexeddb.TypeString},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var AgentRunMetadataSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_subject", KeyPath: []string{"subject_id"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "provider_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "subject_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "credential_subject_id", Type: indexeddb.TypeString},
		{Name: "permissions_json", Type: indexeddb.TypeString},
		{Name: "idempotency_key", Type: indexeddb.TypeString},
		{Name: "model", Type: indexeddb.TypeString},
		{Name: "session_ref", Type: indexeddb.TypeString},
		{Name: "created_by_json", Type: indexeddb.TypeString},
		{Name: "tool_source", Type: indexeddb.TypeString},
		{Name: "tools_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "revoked_at", Type: indexeddb.TypeTime},
	},
}

var AgentRunIdempotencySchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_run_id", KeyPath: []string{"run_id"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "run_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "subject_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "provider_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "idempotency_key", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_at", Type: indexeddb.TypeTime},
	},
}

var AgentRunEventsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_run", KeyPath: []string{"run_id"}},
		{Name: "by_run_seq", KeyPath: []string{"run_id", "seq"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "run_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "seq", Type: indexeddb.TypeInt, NotNull: true},
		{Name: "type", Type: indexeddb.TypeString, NotNull: true},
		{Name: "source", Type: indexeddb.TypeString},
		{Name: "visibility", Type: indexeddb.TypeString},
		{Name: "data_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
	},
}
