package coredata

import (
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

var AppRegistryPublishSessionsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_app", KeyPath: []string{"app"}},
		{Name: "by_app_state", KeyPath: []string{"app", "state"}},
		{Name: "by_dedupe_key", KeyPath: []string{"dedupe_key"}, Unique: true},
		{Name: "by_app_version", KeyPath: []string{"app", "version"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "app", Type: idb.TypeString, NotNull: true},
		{Name: "registry", Type: idb.TypeString, NotNull: true},
		{Name: "version", Type: idb.TypeString, NotNull: true},
		{Name: "dedupe_key", Type: idb.TypeString, NotNull: true, Unique: true},
		{Name: "declaration_digest", Type: idb.TypeString, NotNull: true},
		{Name: "declaration_json", Type: idb.TypeJSON, NotNull: true},
		{Name: "state", Type: idb.TypeString, NotNull: true},
		{Name: "publisher_subject_id", Type: idb.TypeString, NotNull: true},
		{Name: "artifacts_json", Type: idb.TypeJSON},
		{Name: "upload_leases_json", Type: idb.TypeJSON},
		{Name: "staging_prefix", Type: idb.TypeString, NotNull: true},
		{Name: "failure_reason", Type: idb.TypeString},
		{Name: "publish_started_at", Type: idb.TypeTime},
		{Name: "created_at", Type: idb.TypeTime, NotNull: true},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
		{Name: "revision", Type: idb.TypeInt},
		{Name: "published_at", Type: idb.TypeTime},
		{Name: "staging_marked_stale_at", Type: idb.TypeTime},
		{Name: "finalize_claim_token", Type: idb.TypeString},
		{Name: "finalize_claim_expires_at", Type: idb.TypeTime},
		{Name: "finalize_published_at", Type: idb.TypeTime},
	},
}
