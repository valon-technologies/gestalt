package coredata

import (
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

const (
	StoreUsers                          = "users"
	StoreManagedSubjects                = "managed_subjects"
	StoreAuthorizationDynamicFragments  = "authz_dynamic_fragments"
	StoreAppSHAs                        = "app_shas"
	StoreAppVersionChangeRequests       = "app_version_change_requests"
	StoreAppVersionInstallLocks         = "app_version_install_locks"
	StoreGestaltdSourceVersionState     = "gestaltd_source_version_state"
	StoreAppRollouts                    = "app_rollouts"
	StoreAppInstanceMaterializations    = "app_instance_materializations"
	StoreAppAutoDeploySettings          = "app_auto_deploy_settings"
	StoreAppVersionRolloutOutcomes      = "app_version_rollout_outcomes"
	StoreGestaltdInstanceHeartbeats     = "gestaltd_instance_heartbeats"
	StoreAppVersionRecoveryObservations = "app_version_recovery_observations"
	StoreRemoteRegistrations            = "remote_registrations"
	StoreRemoteProviders                = "remote_providers"
	StoreConnectionInstancePreferences  = "connection_instance_preferences"
	StoreAppAccessProfiles              = "app_access_profiles"
	StoreSCIMUsers                      = "scim_users"
	StoreSCIMProjectionIntents          = "scim_projection_intents"
	StoreSCIMGroups                     = "scim_groups"
)

var SCIMUsersSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_client", KeyPath: []string{"client_id"}},
		{Name: "by_core_user", KeyPath: []string{"core_user_id"}},
		{Name: "by_user_name_key", KeyPath: []string{"user_name_key"}, Unique: true},
		{Name: "by_external_id_key", KeyPath: []string{"external_id_key"}, Unique: true},
		{Name: "by_email_key", KeyPath: []string{"email_key"}, Unique: true},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "client_id", Type: idb.TypeString, NotNull: true},
		{Name: "core_user_id", Type: idb.TypeString, NotNull: true},
		{Name: "authoritative_domain", Type: idb.TypeString},
		{Name: "user_name_key", Type: idb.TypeString},
		{Name: "external_id_key", Type: idb.TypeString},
		{Name: "email_key", Type: idb.TypeString},
		{Name: "active", Type: idb.TypeBool, NotNull: true},
		{Name: "deleted", Type: idb.TypeBool, NotNull: true},
		{Name: "version", Type: idb.TypeInt, NotNull: true},
		{Name: "resource", Type: idb.TypeJSON, NotNull: true},
		{Name: "applied_relationships", Type: idb.TypeJSON},
		{Name: "last_operation_fingerprint", Type: idb.TypeString},
		{Name: "created_at", Type: idb.TypeTime, NotNull: true},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
		{Name: "deleted_at", Type: idb.TypeTime},
	},
}

var SCIMProjectionIntentsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_user", KeyPath: []string{"user_id"}, Unique: true},
		{Name: "by_core_user", KeyPath: []string{"core_user_id"}},
		{Name: "by_client", KeyPath: []string{"client_id"}},
		{Name: "by_next_attempt", KeyPath: []string{"next_attempt_at"}},
		{Name: "by_user_name_key", KeyPath: []string{"user_name_key"}, Unique: true},
		{Name: "by_external_id_key", KeyPath: []string{"external_id_key"}, Unique: true},
		{Name: "by_email_key", KeyPath: []string{"email_key"}, Unique: true},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "user_id", Type: idb.TypeString, NotNull: true},
		{Name: "client_id", Type: idb.TypeString, NotNull: true},
		{Name: "core_user_id", Type: idb.TypeString, NotNull: true},
		{Name: "authoritative_domain", Type: idb.TypeString},
		{Name: "user_name_key", Type: idb.TypeString},
		{Name: "external_id_key", Type: idb.TypeString},
		{Name: "email_key", Type: idb.TypeString},
		{Name: "base_version", Type: idb.TypeInt, NotNull: true},
		{Name: "next_version", Type: idb.TypeInt, NotNull: true},
		{Name: "proposed", Type: idb.TypeJSON, NotNull: true},
		{Name: "proposed_deleted", Type: idb.TypeBool, NotNull: true},
		{Name: "from_relationships", Type: idb.TypeJSON},
		{Name: "to_relationships", Type: idb.TypeJSON},
		{Name: "operation_fingerprint", Type: idb.TypeString},
		{Name: "attempt_count", Type: idb.TypeInt, NotNull: true},
		{Name: "next_attempt_at", Type: idb.TypeTime, NotNull: true},
		{Name: "last_error", Type: idb.TypeString},
		{Name: "created_at", Type: idb.TypeTime, NotNull: true},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
	},
}

// SCIMGroupsSchema keeps the committed resource and any pending replacement
// together so projection recovery does not require a separate intent store.
var SCIMGroupsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_client", KeyPath: []string{"client_id"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "client_id", Type: idb.TypeString, NotNull: true},
		{Name: "version", Type: idb.TypeInt, NotNull: true},
		{Name: "deleted", Type: idb.TypeBool, NotNull: true},
		{Name: "resource", Type: idb.TypeJSON, NotNull: true},
		{Name: "pending_resource", Type: idb.TypeJSON},
		{Name: "pending_deleted", Type: idb.TypeBool},
		{Name: "pending_version", Type: idb.TypeInt},
		{Name: "pending_fingerprint", Type: idb.TypeString},
		{Name: "pending_affected_users", Type: idb.TypeJSON},
		{Name: "pending_attempt_count", Type: idb.TypeInt},
		{Name: "pending_next_attempt_at", Type: idb.TypeTime},
		{Name: "last_operation_fingerprint", Type: idb.TypeString},
		{Name: "created_at", Type: idb.TypeTime, NotNull: true},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
	},
}

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
		{Name: "from_version_deployable_until", Type: idb.TypeTime},
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

// Keep existing-store schemas limited to the metadata already deployed.
// relationaldb persists complete record blobs, including undeclared fields, but
// requires exact schema metadata equality when a store is opened.
var GestaltdSourceVersionStateSchema = idb.ObjectStoreOptions{
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "current_source_version", Type: idb.TypeString},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
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
		{Name: "target_source_version", Type: idb.TypeString},
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
		{Name: "source_version", Type: idb.TypeString},
		{Name: "app", Type: idb.TypeString, NotNull: true},
		{Name: "version", Type: idb.TypeString, NotNull: true},
		{Name: "acknowledged_at", Type: idb.TypeTime, NotNull: true},
		{Name: "materialized_at", Type: idb.TypeTime},
		{Name: "stopped_at", Type: idb.TypeTime},
		{Name: "restarted_at", Type: idb.TypeTime},
		{Name: "attempt_count", Type: idb.TypeInt},
		{Name: "last_error_at", Type: idb.TypeTime},
		{Name: "last_error_message", Type: idb.TypeString},
	},
}

var AppAutoDeploySettingsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_enabled", KeyPath: []string{"enabled"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "app", Type: idb.TypeString, NotNull: true, Unique: true},
		{Name: "enabled", Type: idb.TypeBool, NotNull: true},
		{Name: "pending_version", Type: idb.TypeString},
		{Name: "last_seen_version", Type: idb.TypeString},
		{Name: "last_error", Type: idb.TypeString},
		{Name: "last_failed_rollout_at", Type: idb.TypeTime},
	},
}

var AppVersionRolloutOutcomesSchema = idb.ObjectStoreOptions{
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "app", Type: idb.TypeString, NotNull: true},
		{Name: "version", Type: idb.TypeString, NotNull: true},
		{Name: "completed_at", Type: idb.TypeTime},
		{Name: "failed_at", Type: idb.TypeTime},
	},
}

var GestaltdInstanceHeartbeatsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_instance", KeyPath: []string{"instance_id"}, Unique: true},
		{Name: "by_source_version", KeyPath: []string{"source_version"}},
		{Name: "by_heartbeat_at", KeyPath: []string{"heartbeat_at"}},
		{Name: "by_source_version_heartbeat_at", KeyPath: []string{"source_version", "heartbeat_at"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "instance_id", Type: idb.TypeString, NotNull: true, Unique: true},
		{Name: "source_version", Type: idb.TypeString, NotNull: true},
		{Name: "started_at", Type: idb.TypeTime, NotNull: true},
		{Name: "heartbeat_at", Type: idb.TypeTime, NotNull: true},
		{Name: "apps", Type: idb.TypeJSON, NotNull: true},
	},
}

var AppVersionRecoveryObservationsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_app", KeyPath: []string{"app"}},
		{Name: "by_app_version", KeyPath: []string{"app", "version"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "app", Type: idb.TypeString, NotNull: true},
		{Name: "version", Type: idb.TypeString, NotNull: true},
		{Name: "recovered_at", Type: idb.TypeTime, NotNull: true},
		{Name: "source_version", Type: idb.TypeString, NotNull: true},
		{Name: "live_instances", Type: idb.TypeInt, NotNull: true},
		{Name: "minimum_healthy_instances", Type: idb.TypeInt, NotNull: true},
	},
}

var RemoteRegistrationsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_owner_subject", KeyPath: []string{"owner_subject_id"}, Unique: true},
	},
}

var RemoteProvidersSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_registration", KeyPath: []string{"registration_id"}},
	},
}

var ConnectionInstancePreferencesSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_subject_connection", KeyPath: []string{"subject_id", "connection_id"}, Unique: true},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "subject_id", Type: idb.TypeString, NotNull: true},
		{Name: "connection_id", Type: idb.TypeString, NotNull: true},
		{Name: "instance", Type: idb.TypeString, NotNull: true},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
	},
}

var AppAccessProfilesSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_subject", KeyPath: []string{"subject_id"}},
		{Name: "by_app", KeyPath: []string{"app"}},
		{Name: "by_subject_app", KeyPath: []string{"subject_id", "app"}, Unique: true},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "subject_id", Type: idb.TypeString, NotNull: true},
		{Name: "app", Type: idb.TypeString, NotNull: true},
		{Name: "enabled_operations", Type: idb.TypeString},
		{Name: "defaults_initialized", Type: idb.TypeBool, NotNull: true},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
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
