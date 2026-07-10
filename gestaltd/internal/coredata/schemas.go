package coredata

import (
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

const (
	StoreUsers                         = "users"
	StoreManagedSubjects               = "managed_subjects"
	StoreAuthorizationDynamicFragments = "authz_dynamic_fragments"
	StoreAppSHAs                       = "app_shas"
	StoreAppInstallations              = "app_installations"
	StoreAppInstallationEvents         = "app_installation_events"
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

var AppInstallationsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_desired_state", KeyPath: []string{"desired_state"}},
		{Name: "by_registry", KeyPath: []string{"registry"}},
		{Name: "by_resolved_version", KeyPath: []string{"resolved_version"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "app_name", Type: idb.TypeString, NotNull: true, Unique: true},
		{Name: "desired_version_constraint", Type: idb.TypeString},
		{Name: "resolved_version", Type: idb.TypeString},
		{Name: "source_ref", Type: idb.TypeString},
		{Name: "registry", Type: idb.TypeString},
		{Name: "provider_release_url", Type: idb.TypeString},
		{Name: "artifact_checksums_json", Type: idb.TypeJSON},
		{Name: "desired_state", Type: idb.TypeString, NotNull: true},
		{Name: "active_since", Type: idb.TypeTime},
		{Name: "previous_resolved_version", Type: idb.TypeString},
		{Name: "installed_by", Type: idb.TypeString},
		{Name: "installed_at", Type: idb.TypeTime},
		{Name: "updated_at", Type: idb.TypeTime},
	},
}

var AppInstallationEventsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_app_name", KeyPath: []string{"app_name"}},
		{Name: "by_app_created", KeyPath: []string{"app_name", "created_at"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "event_id", Type: idb.TypeString, NotNull: true, Unique: true},
		{Name: "app_name", Type: idb.TypeString, NotNull: true},
		{Name: "from_version", Type: idb.TypeString},
		{Name: "to_version", Type: idb.TypeString},
		{Name: "event_type", Type: idb.TypeString, NotNull: true},
		{Name: "actor", Type: idb.TypeString},
		{Name: "created_at", Type: idb.TypeTime, NotNull: true},
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
