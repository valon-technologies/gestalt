package coredata

import "github.com/valon-technologies/gestalt/server/core/indexeddb"

const (
	StoreUsers             = "users"
	StoreIntegrationTokens = "integration_tokens"
	StoreAPITokens         = "api_tokens"
)

var UsersSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_email", KeyPath: []string{"email"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "email", Type: indexeddb.TypeString, NotNull: true, Unique: true},
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
		{Name: "by_lookup", KeyPath: []string{"user_id", "integration", "connection", "instance"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "user_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "integration", Type: indexeddb.TypeString, NotNull: true},
		{Name: "connection", Type: indexeddb.TypeString, NotNull: true},
		{Name: "instance", Type: indexeddb.TypeString},
		{Name: "access_token_sealed", Type: indexeddb.TypeString},
		{Name: "refresh_token_sealed", Type: indexeddb.TypeString},
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
