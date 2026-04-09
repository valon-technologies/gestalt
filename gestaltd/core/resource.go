package core

import "context"

// ResourceCapability identifies a storage capability that a resource provider
// can expose.
type ResourceCapability string

const (
	ResourceCapabilityKeyValue  ResourceCapability = "key_value"
	ResourceCapabilitySQL       ResourceCapability = "sql"
	ResourceCapabilityBlobStore ResourceCapability = "blob_store"
)

// ResourceProvider represents a started resource instance.
type ResourceProvider interface {
	Name() string
	Capabilities() []ResourceCapability
	Ping(ctx context.Context) error
	Close() error
}

// ResourceHandle holds a resolved resource binding for a single consumer. At
// most one of KV, SQL, or Blob is non-nil depending on the requested
// capability.
type ResourceHandle struct {
	ResourceName string
	Namespace    string
	Capability   ResourceCapability
	KV           KeyValueStore
	SQL          SQLStore
	Blob         BlobStore
}

// KeyValueStore is the consumer interface for key-value storage.
type KeyValueStore interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Put(ctx context.Context, key string, value []byte) error
	PutWithTTL(ctx context.Context, key string, value []byte, ttlSeconds int64) error
	Delete(ctx context.Context, key string) (deleted bool, err error)
	List(ctx context.Context, prefix string, cursor string, limit int32) (entries []KVEntry, nextCursor string, err error)
}

// KVEntry is a single key-value pair.
type KVEntry struct {
	Key   string
	Value []byte
}

// SQLStore is the consumer interface for SQL storage.
type SQLStore interface {
	Query(ctx context.Context, query string, params ...any) (*SQLRows, error)
	Exec(ctx context.Context, query string, params ...any) (SQLExecResult, error)
	Migrate(ctx context.Context, migrations []SQLMigration) error
}

// SQLRows holds the result of a SQL query.
type SQLRows struct {
	Columns []string
	Rows    [][]any
}

// SQLExecResult holds the result of a SQL exec statement.
type SQLExecResult struct {
	RowsAffected int64
	LastInsertID int64
}

// SQLMigration describes a single forward migration.
type SQLMigration struct {
	Version     int32
	Description string
	UpSQL       string
}

// BlobStore is the consumer interface for blob storage.
type BlobStore interface {
	Get(ctx context.Context, key string) (data []byte, contentType string, err error)
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string, cursor string, limit int32) (entries []BlobEntry, nextCursor string, err error)
}

// BlobEntry describes a single blob.
type BlobEntry struct {
	Key         string
	Size        int64
	ContentType string
}
