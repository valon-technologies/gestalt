package coredata

import (
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

const (
	StoreUsers                         = "users"
	StoreManagedSubjects               = "managed_subjects"
	StoreAuthorizationDynamicFragments = "authz_dynamic_fragments"
	StoreAppSHAs                       = "app_shas"
	StoreAppVersionCatalog             = "app_version_catalog"
)

var AppSHAsSchema = idb.ObjectStoreOptions{
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "sha", Type: idb.TypeString, NotNull: true},
	},
}

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

var AppVersionCatalogSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_app", KeyPath: []string{"app"}},
		{Name: "by_app_timestamp", KeyPath: []string{"app", "timestamp"}},
		{Name: "by_app_version", KeyPath: []string{"app", "version"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "app", Type: idb.TypeString, NotNull: true},
		{Name: "version", Type: idb.TypeString, NotNull: true},
		{Name: "type", Type: idb.TypeString, NotNull: true},
		{Name: "actor", Type: idb.TypeString},
		{Name: "timestamp", Type: idb.TypeTime, NotNull: true},
		{Name: "metadata_json", Type: idb.TypeJSON},
	},
}

var AuthorizationDynamicFragmentsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_owner", KeyPath: []string{"owner_kind", "owner_id"}, Unique: true},
		{Name: "by_scope", KeyPath: []string{"scope"}},
		{Name: "by_app", KeyPath: []string{"app"}},
		{Name: "by_status", KeyPath: []string{"status"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "owner_kind", Type: idb.TypeString, NotNull: true},
		{Name: "owner_id", Type: idb.TypeString, NotNull: true},
		{Name: "scope", Type: idb.TypeString, NotNull: true},
		{Name: "app", Type: idb.TypeString},
		{Name: "version", Type: idb.TypeInt},
		{Name: "status", Type: idb.TypeString},
		{Name: "resource_types_json", Type: idb.TypeString},
		{Name: "relationships_json", Type: idb.TypeString},
		{Name: "audit_json", Type: idb.TypeString},
		{Name: "created_at", Type: idb.TypeTime},
		{Name: "updated_at", Type: idb.TypeTime},
	},
}
