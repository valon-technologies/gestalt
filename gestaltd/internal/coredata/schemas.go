package coredata

import (
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

const (
	StoreUsers           = "users"
	StoreAPITokens       = "api_tokens"
	StoreManagedSubjects = "managed_subjects"
)

var UsersSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_email", KeyPath: []string{"email"}, Unique: true},
		{Name: "by_normalized_email", KeyPath: []string{"normalized_email"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "email", Type: idb.TypeString, NotNull: true, Unique: true},
		{Name: "normalized_email", Type: idb.TypeString},
		{Name: "display_name", Type: idb.TypeString},
		{Name: "created_at", Type: idb.TypeTime},
		{Name: "updated_at", Type: idb.TypeTime},
	},
}

var APITokensSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_hash", KeyPath: []string{"hashed_token"}, Unique: true},
		{Name: "by_owner", KeyPath: []string{"owner_kind", "owner_id"}},
		{Name: "by_owner_id", KeyPath: []string{"id", "owner_kind", "owner_id"}, Unique: true},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "owner_kind", Type: idb.TypeString},
		{Name: "owner_id", Type: idb.TypeString},
		{Name: "credential_subject_id", Type: idb.TypeString},
		{Name: "name", Type: idb.TypeString},
		{Name: "hashed_token", Type: idb.TypeString, NotNull: true, Unique: true},
		{Name: "scopes", Type: idb.TypeString},
		{Name: "permissions_json", Type: idb.TypeString},
		{Name: "expires_at", Type: idb.TypeTime},
		{Name: "created_at", Type: idb.TypeTime},
		{Name: "updated_at", Type: idb.TypeTime},
	},
}

var ManagedSubjectsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_kind", KeyPath: []string{"kind"}},
		{Name: "by_kind_deleted", KeyPath: []string{"kind", "deleted"}},
		{Name: "by_created_by", KeyPath: []string{"created_by_subject_id"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "subject_id", Type: idb.TypeString, NotNull: true, Unique: true},
		{Name: "kind", Type: idb.TypeString, NotNull: true},
		{Name: "display_name", Type: idb.TypeString},
		{Name: "description", Type: idb.TypeString},
		{Name: "created_by_subject_id", Type: idb.TypeString},
		{Name: "deleted", Type: idb.TypeBool},
		{Name: "created_at", Type: idb.TypeTime},
		{Name: "updated_at", Type: idb.TypeTime},
		{Name: "deleted_at", Type: idb.TypeTime},
	},
}
