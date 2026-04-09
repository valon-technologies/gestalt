package gestalt

import (
	"context"
	"time"
)

// DatastoreCapability identifies a storage capability that a resource provider
// can implement and that plugins can consume.
type DatastoreCapability string

const (
	CapabilityKeyValue  DatastoreCapability = "key_value"
	CapabilitySQL       DatastoreCapability = "sql"
	CapabilityBlobStore DatastoreCapability = "blob_store"
)

// KeyValueStore is the consumer interface for key-value storage. Plugins
// receive an implementation of this interface when they declare a key_value
// resource dependency. The namespace is injected by the host and is not
// exposed to consumers.
type KeyValueStore interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Put(ctx context.Context, key string, value []byte) error
	PutWithTTL(ctx context.Context, key string, value []byte, ttlSeconds int64) error
	Delete(ctx context.Context, key string) (deleted bool, err error)
	List(ctx context.Context, prefix string, cursor string, limit int32) (entries []KVEntry, nextCursor string, err error)
}

// KVEntry is a single key-value pair returned by [KeyValueStore.List].
type KVEntry struct {
	Key   string
	Value []byte
}

// SQLStore is the consumer interface for SQL storage. Plugins receive an
// implementation of this interface when they declare a sql resource dependency.
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

// SQLMigration describes a single forward migration to be applied by the
// resource provider.
type SQLMigration struct {
	Version     int32
	Description string
	UpSQL       string
}

// BlobStore is the consumer interface for blob storage. Plugins receive an
// implementation of this interface when they declare a blob_store resource
// dependency.
type BlobStore interface {
	Get(ctx context.Context, key string) (data []byte, contentType string, err error)
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string, cursor string, limit int32) (entries []BlobEntry, nextCursor string, err error)
}

// BlobEntry describes a single blob returned by [BlobStore.List].
type BlobEntry struct {
	Key          string
	Size         int64
	ContentType  string
	LastModified time.Time
}
