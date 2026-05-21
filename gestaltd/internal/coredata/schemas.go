package coredata

import (
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const (
	StoreUsers                         = "users"
	StoreAPITokens                     = "api_tokens"
	StoreManagedSubjects               = "managed_subjects"
	StoreAuthorizationDynamicFragments = "authz_dynamic_fragments"
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

var APITokensSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_hash", KeyPath: []string{"hashed_token"}, Unique: true},
		{Name: "by_owner", KeyPath: []string{"owner_kind", "owner_id"}},
		{Name: "by_owner_id", KeyPath: []string{"id", "owner_kind", "owner_id"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "owner_kind", Type: indexeddb.TypeString},
		{Name: "owner_id", Type: indexeddb.TypeString},
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

var ManagedSubjectsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_kind", KeyPath: []string{"kind"}},
		{Name: "by_kind_deleted", KeyPath: []string{"kind", "deleted"}},
		{Name: "by_created_by", KeyPath: []string{"created_by_subject_id"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "subject_id", Type: indexeddb.TypeString, NotNull: true, Unique: true},
		{Name: "kind", Type: indexeddb.TypeString, NotNull: true},
		{Name: "display_name", Type: indexeddb.TypeString},
		{Name: "description", Type: indexeddb.TypeString},
		{Name: "created_by_subject_id", Type: indexeddb.TypeString},
		{Name: "deleted", Type: indexeddb.TypeBool},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
		{Name: "deleted_at", Type: indexeddb.TypeTime},
	},
}

var AuthorizationDynamicFragmentsSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_owner", KeyPath: []string{"owner_kind", "owner_id"}, Unique: true},
		{Name: "by_scope", KeyPath: []string{"scope"}},
		{Name: "by_plugin", KeyPath: []string{"plugin"}},
		{Name: "by_status", KeyPath: []string{"status"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "owner_kind", Type: indexeddb.TypeString, NotNull: true},
		{Name: "owner_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "scope", Type: indexeddb.TypeString, NotNull: true},
		{Name: "plugin", Type: indexeddb.TypeString},
		{Name: "version", Type: indexeddb.TypeInt},
		{Name: "status", Type: indexeddb.TypeString},
		{Name: "resource_types_json", Type: indexeddb.TypeString},
		{Name: "relationships_json", Type: indexeddb.TypeString},
		{Name: "audit_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}
