package coredata

import "github.com/valon-technologies/gestalt/server/core/indexeddb"

const (
	StoreUsers                          = "users"
	StoreIntegrationTokens              = "integration_tokens"
	StoreAPITokens                      = "api_tokens"
	StoreManagedIdentities              = "managed_identities"
	StoreManagedIdentityMemberships     = "managed_identity_memberships"
	StoreManagedIdentityGrants          = "managed_identity_grants"
	StorePluginAuthorizationMemberships = "plugin_authorization_memberships"
	StoreAdminAuthorizationMemberships  = "admin_authorization_memberships"
	StorePrincipals                     = "principals"
	StoreUserProfiles                   = "user_profiles"
	StoreServiceAccounts                = "service_accounts"
	StoreServiceAccountManagementGrants = "service_account_management_grants"
	StoreWorkspaceRoles                 = "workspace_roles"
	StorePrincipalPluginAccess          = "principal_plugin_access"
	StoreServiceAccountDelegations      = "service_account_delegations"
	StoreAPITokenAccess                 = "api_token_access"
	StoreExternalCredentials            = "external_credentials"
	StoreServiceAccountAuthBindings     = "service_account_auth_bindings"
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

var IntegrationTokensSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_user", KeyPath: []string{"user_id"}},
		{Name: "by_user_integration", KeyPath: []string{"user_id", "integration"}},
		{Name: "by_user_connection", KeyPath: []string{"user_id", "integration", "connection"}},
		{Name: "by_lookup", KeyPath: []string{"user_id", "integration", "connection", "instance"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "user_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "integration", Type: indexeddb.TypeString, NotNull: true},
		{Name: "connection", Type: indexeddb.TypeString, NotNull: true},
		{Name: "instance", Type: indexeddb.TypeString},
		{Name: "access_token_encrypted", Type: indexeddb.TypeString},
		{Name: "refresh_token_encrypted", Type: indexeddb.TypeString},
		{Name: "scopes", Type: indexeddb.TypeString},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "last_refreshed_at", Type: indexeddb.TypeTime},
		{Name: "refresh_error_count", Type: indexeddb.TypeInt},
		{Name: "metadata_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var APITokensSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_hash", KeyPath: []string{"hashed_token"}, Unique: true},
		{Name: "by_user", KeyPath: []string{"user_id"}},
		{Name: "by_user_id", KeyPath: []string{"id", "user_id"}, Unique: true},
		{Name: "by_owner", KeyPath: []string{"owner_kind", "owner_id"}},
		{Name: "by_owner_id", KeyPath: []string{"id", "owner_kind", "owner_id"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "user_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "owner_kind", Type: indexeddb.TypeString},
		{Name: "owner_id", Type: indexeddb.TypeString},
		{Name: "name", Type: indexeddb.TypeString},
		{Name: "hashed_token", Type: indexeddb.TypeString, NotNull: true, Unique: true},
		{Name: "scopes", Type: indexeddb.TypeString},
		{Name: "permissions_json", Type: indexeddb.TypeString},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var ManagedIdentitiesSchema = indexeddb.ObjectStoreSchema{
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "display_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var ManagedIdentityMembershipsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_identity", KeyPath: []string{"identity_id"}},
		{Name: "by_user", KeyPath: []string{"user_id"}},
		{Name: "by_identity_user", KeyPath: []string{"identity_id", "user_id"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "identity_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "user_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "email", Type: indexeddb.TypeString, NotNull: true},
		{Name: "role", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var ManagedIdentityGrantsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_identity", KeyPath: []string{"identity_id"}},
		{Name: "by_identity_plugin", KeyPath: []string{"identity_id", "plugin"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "identity_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "plugin", Type: indexeddb.TypeString, NotNull: true},
		{Name: "operations_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var PluginAuthorizationMembershipsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_plugin", KeyPath: []string{"plugin"}},
		{Name: "by_plugin_user", KeyPath: []string{"plugin", "user_id"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "plugin", Type: indexeddb.TypeString, NotNull: true},
		{Name: "user_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "email", Type: indexeddb.TypeString, NotNull: true},
		{Name: "role", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var AdminAuthorizationMembershipsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_user", KeyPath: []string{"user_id"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "user_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "email", Type: indexeddb.TypeString, NotNull: true},
		{Name: "role", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var PrincipalsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_kind", KeyPath: []string{"kind"}},
		{Name: "by_status", KeyPath: []string{"status"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "kind", Type: indexeddb.TypeString, NotNull: true},
		{Name: "status", Type: indexeddb.TypeString, NotNull: true},
		{Name: "display_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var UserProfilesSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_email", KeyPath: []string{"email"}, Unique: true},
		{Name: "by_normalized_email", KeyPath: []string{"normalized_email"}, Unique: true},
		{Name: "by_auth_subject", KeyPath: []string{"auth_provider", "auth_subject"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "principal_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "email", Type: indexeddb.TypeString, NotNull: true, Unique: true},
		{Name: "normalized_email", Type: indexeddb.TypeString, NotNull: true, Unique: true},
		{Name: "auth_provider", Type: indexeddb.TypeString},
		{Name: "auth_subject", Type: indexeddb.TypeString},
		{Name: "avatar_url", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var ServiceAccountsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_name", KeyPath: []string{"name"}, Unique: true},
		{Name: "by_creator", KeyPath: []string{"created_by_principal_id"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "principal_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "name", Type: indexeddb.TypeString, NotNull: true, Unique: true},
		{Name: "description", Type: indexeddb.TypeString},
		{Name: "created_by_principal_id", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var ServiceAccountManagementGrantsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_member", KeyPath: []string{"member_principal_id"}},
		{Name: "by_target", KeyPath: []string{"target_service_account_principal_id"}},
		{Name: "by_member_target", KeyPath: []string{"member_principal_id", "target_service_account_principal_id"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "member_principal_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "target_service_account_principal_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "role", Type: indexeddb.TypeString, NotNull: true},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var WorkspaceRolesSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_principal", KeyPath: []string{"principal_id"}},
		{Name: "by_principal_role", KeyPath: []string{"principal_id", "role"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "principal_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "role", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var PrincipalPluginAccessSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_principal", KeyPath: []string{"principal_id"}},
		{Name: "by_plugin", KeyPath: []string{"plugin"}},
		{Name: "by_principal_plugin", KeyPath: []string{"principal_id", "plugin"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "principal_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "plugin", Type: indexeddb.TypeString, NotNull: true},
		{Name: "invoke_all_operations", Type: indexeddb.TypeBool, NotNull: true},
		{Name: "operations_json", Type: indexeddb.TypeString},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var ServiceAccountDelegationsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_actor", KeyPath: []string{"actor_user_principal_id"}},
		{Name: "by_target", KeyPath: []string{"target_service_account_principal_id"}},
		{Name: "by_actor_target_plugin", KeyPath: []string{"actor_user_principal_id", "target_service_account_principal_id", "plugin"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "actor_user_principal_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "target_service_account_principal_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "plugin", Type: indexeddb.TypeString},
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

var ExternalCredentialsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_principal", KeyPath: []string{"principal_id"}},
		{Name: "by_principal_plugin", KeyPath: []string{"principal_id", "plugin"}},
		{Name: "by_principal_connection", KeyPath: []string{"principal_id", "plugin", "connection"}},
		{Name: "by_lookup", KeyPath: []string{"principal_id", "plugin", "connection", "instance"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "principal_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "plugin", Type: indexeddb.TypeString, NotNull: true},
		{Name: "connection", Type: indexeddb.TypeString, NotNull: true},
		{Name: "instance", Type: indexeddb.TypeString, NotNull: true},
		{Name: "auth_type", Type: indexeddb.TypeString, NotNull: true},
		{Name: "payload_encrypted", Type: indexeddb.TypeString},
		{Name: "scopes", Type: indexeddb.TypeString},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "last_refreshed_at", Type: indexeddb.TypeTime},
		{Name: "refresh_error_count", Type: indexeddb.TypeInt},
		{Name: "metadata_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var ServiceAccountAuthBindingsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_service_account", KeyPath: []string{"service_account_principal_id"}},
		{Name: "by_binding_kind", KeyPath: []string{"binding_kind"}},
		{Name: "by_lookup", KeyPath: []string{"binding_kind", "lookup_key"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "service_account_principal_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "binding_kind", Type: indexeddb.TypeString, NotNull: true},
		{Name: "lookup_key", Type: indexeddb.TypeString, NotNull: true},
		{Name: "binding_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}
