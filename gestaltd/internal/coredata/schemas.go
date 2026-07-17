package coredata

import (
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

const (
	StoreUsers                         = "users"
	StoreManagedSubjects               = "managed_subjects"
	StoreAuthorizationDynamicFragments = "authz_dynamic_fragments"
	StoreAppSHAs                       = "app_shas"
	StoreAppVersionChangeRequests      = "app_version_change_requests"
	StoreAppVersionInstallLocks        = "app_version_install_locks"
	StoreAppRollouts                   = "app_rollouts"
	StoreAppInstanceMaterializations   = "app_instance_materializations"
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

var AppVersionChangeRequestsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_app", KeyPath: []string{"app"}},
		{Name: "by_app_timestamp", KeyPath: []string{"app", "timestamp"}},
		{Name: "by_app_to_version", KeyPath: []string{"app", "to_version"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "app", Type: idb.TypeString, NotNull: true},
		{Name: "from_version", Type: idb.TypeString, NotNull: true},
		{Name: "to_version", Type: idb.TypeString, NotNull: true},
		{Name: "actor", Type: idb.TypeString},
		{Name: "timestamp", Type: idb.TypeTime, NotNull: true},
		{Name: "metadata_json", Type: idb.TypeJSON},
	},
}

var AppVersionInstallLocksSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_app_version", KeyPath: []string{"app", "version"}, Unique: true},
		{Name: "by_expires_at", KeyPath: []string{"expires_at"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "app", Type: idb.TypeString, NotNull: true},
		{Name: "version", Type: idb.TypeString, NotNull: true},
		{Name: "holder", Type: idb.TypeString, NotNull: true},
		{Name: "acquired_at", Type: idb.TypeTime, NotNull: true},
		{Name: "expires_at", Type: idb.TypeTime, NotNull: true},
	},
}

var AppRolloutsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_state", KeyPath: []string{"state"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "app", Type: idb.TypeString, NotNull: true, Unique: true},
		{Name: "version", Type: idb.TypeString, NotNull: true},
		{Name: "state", Type: idb.TypeString, NotNull: true},
		{Name: "created_at", Type: idb.TypeTime, NotNull: true},
		{Name: "enrollment_ends_at", Type: idb.TypeTime, NotNull: true},
		{Name: "deadline", Type: idb.TypeTime, NotNull: true},
		{Name: "completed_at", Type: idb.TypeTime},
		{Name: "failed_at", Type: idb.TypeTime},
	},
}

var AppInstanceMaterializationsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_instance", KeyPath: []string{"instance_id"}},
		{Name: "by_instance_app_version", KeyPath: []string{"instance_id", "app", "version"}, Unique: true},
		{Name: "by_app_version", KeyPath: []string{"app", "version"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "instance_id", Type: idb.TypeString, NotNull: true},
		{Name: "app", Type: idb.TypeString, NotNull: true},
		{Name: "version", Type: idb.TypeString, NotNull: true},
		{Name: "acknowledged_at", Type: idb.TypeTime, NotNull: true},
		{Name: "materialized_at", Type: idb.TypeTime},
		{Name: "stopped_at", Type: idb.TypeTime},
		{Name: "restarted_at", Type: idb.TypeTime},
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
