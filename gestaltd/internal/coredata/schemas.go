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
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "user_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "name", Type: indexeddb.TypeString},
		{Name: "hashed_token", Type: indexeddb.TypeString, NotNull: true, Unique: true},
		{Name: "scopes", Type: indexeddb.TypeString},
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
