package coredata

import (
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const (
	StoreUsers                   = "users"
	StoreAPITokens               = "api_tokens"
	StoreAgentSessionMetadata    = "agent_session_metadata"
	StoreAgentSessionIdempotency = "agent_session_idempotency"
	StoreAgentRunMetadata        = "agent_run_metadata"
	StoreAgentRunIdempotency     = "agent_run_idempotency"
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

var AgentSessionMetadataSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_subject", KeyPath: []string{"subject_id"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "provider_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "subject_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "credential_subject_id", Type: indexeddb.TypeString},
		{Name: "idempotency_key", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "archived_at", Type: indexeddb.TypeTime},
	},
}

var AgentSessionIdempotencySchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_session_id", KeyPath: []string{"session_id"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "session_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "subject_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "provider_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "idempotency_key", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_at", Type: indexeddb.TypeTime},
	},
}

var AgentRunMetadataSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_subject", KeyPath: []string{"subject_id"}},
		{Name: "by_subject_session", KeyPath: []string{"subject_id", "session_id"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "session_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "provider_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "subject_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "credential_subject_id", Type: indexeddb.TypeString},
		{Name: "permissions_json", Type: indexeddb.TypeString},
		{Name: "idempotency_key", Type: indexeddb.TypeString},
		{Name: "tool_refs_json", Type: indexeddb.TypeString},
		{Name: "tool_source", Type: indexeddb.TypeString},
		{Name: "tools_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "revoked_at", Type: indexeddb.TypeTime},
	},
}

var AgentRunIdempotencySchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_run_id", KeyPath: []string{"run_id"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "run_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "subject_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "provider_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "idempotency_key", Type: indexeddb.TypeString, NotNull: true},
		{Name: "created_at", Type: indexeddb.TypeTime},
	},
}
