package coredata

import "github.com/valon-technologies/gestalt/server/core/datastore"

const (
	StoreUsers             = "users"
	StoreIntegrationTokens = "integration_tokens"
	StoreAPITokens         = "api_tokens"
)

var UsersSchema = datastore.ObjectStoreSchema{
	Indexes: []datastore.IndexSchema{
		{Name: "by_email", KeyPath: []string{"email"}, Unique: true},
	},
	Columns: []datastore.ColumnDef{
		{Name: "id", Type: datastore.TypeString, PrimaryKey: true},
		{Name: "email", Type: datastore.TypeString, NotNull: true, Unique: true},
		{Name: "display_name", Type: datastore.TypeString},
		{Name: "created_at", Type: datastore.TypeTime},
		{Name: "updated_at", Type: datastore.TypeTime},
	},
}

var IntegrationTokensSchema = datastore.ObjectStoreSchema{
	Indexes: []datastore.IndexSchema{
		{Name: "by_user", KeyPath: []string{"user_id"}},
		{Name: "by_user_integration", KeyPath: []string{"user_id", "integration"}},
		{Name: "by_user_connection", KeyPath: []string{"user_id", "integration", "connection"}},
		{Name: "by_lookup", KeyPath: []string{"user_id", "integration", "connection", "instance"}},
	},
	Columns: []datastore.ColumnDef{
		{Name: "id", Type: datastore.TypeString, PrimaryKey: true},
		{Name: "user_id", Type: datastore.TypeString, NotNull: true},
		{Name: "integration", Type: datastore.TypeString, NotNull: true},
		{Name: "connection", Type: datastore.TypeString, NotNull: true},
		{Name: "instance", Type: datastore.TypeString},
		{Name: "access_token_sealed", Type: datastore.TypeString},
		{Name: "refresh_token_sealed", Type: datastore.TypeString},
		{Name: "scopes", Type: datastore.TypeString},
		{Name: "expires_at", Type: datastore.TypeTime},
		{Name: "last_refreshed_at", Type: datastore.TypeTime},
		{Name: "refresh_error_count", Type: datastore.TypeInt},
		{Name: "metadata_json", Type: datastore.TypeString},
		{Name: "created_at", Type: datastore.TypeTime},
		{Name: "updated_at", Type: datastore.TypeTime},
	},
}

var APITokensSchema = datastore.ObjectStoreSchema{
	Indexes: []datastore.IndexSchema{
		{Name: "by_hash", KeyPath: []string{"hashed_token"}, Unique: true},
		{Name: "by_user", KeyPath: []string{"user_id"}},
		{Name: "by_user_id", KeyPath: []string{"id", "user_id"}, Unique: true},
	},
	Columns: []datastore.ColumnDef{
		{Name: "id", Type: datastore.TypeString, PrimaryKey: true},
		{Name: "user_id", Type: datastore.TypeString, NotNull: true},
		{Name: "name", Type: datastore.TypeString},
		{Name: "hashed_token", Type: datastore.TypeString, NotNull: true, Unique: true},
		{Name: "scopes", Type: datastore.TypeString},
		{Name: "expires_at", Type: datastore.TypeTime},
		{Name: "created_at", Type: datastore.TypeTime},
		{Name: "updated_at", Type: datastore.TypeTime},
	},
}
