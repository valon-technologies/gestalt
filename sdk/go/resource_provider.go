package gestalt

import "context"

// DatastoreProvider is the base interface for resource plugins (MySQL, Postgres,
// GCS, etc.). Resource providers must also implement one or more capability
// interfaces: [KeyValueDatastoreProvider], [SQLDatastoreProvider], or
// [BlobStoreDatastoreProvider].
type DatastoreProvider interface {
	PluginProvider
	HealthChecker
	Capabilities() []DatastoreCapability
}

// KeyValueDatastoreProvider is implemented by resource plugins that offer
// key-value storage. All methods receive a namespace parameter that the host
// uses to isolate consumers from each other.
type KeyValueDatastoreProvider interface {
	KVGet(ctx context.Context, namespace, key string) (value []byte, found bool, err error)
	KVPut(ctx context.Context, namespace, key string, value []byte, ttlSeconds int64) error
	KVDelete(ctx context.Context, namespace, key string) (deleted bool, err error)
	KVList(ctx context.Context, namespace, prefix, cursor string, limit int32) (entries []KVEntry, nextCursor string, err error)
	KVMigrate(ctx context.Context, namespace string) error
}

// SQLDatastoreProvider is implemented by resource plugins that offer SQL
// storage.
type SQLDatastoreProvider interface {
	SQLQuery(ctx context.Context, namespace, query string, params []SQLValue) (*SQLRows, error)
	SQLExec(ctx context.Context, namespace, query string, params []SQLValue) (SQLExecResult, error)
	SQLMigrate(ctx context.Context, namespace string, migrations []SQLMigration) error
}

// SQLValue is a typed parameter value for SQL operations.
type SQLValue struct {
	Kind  SQLValueKind
	Value any
}

// SQLValueKind identifies the type of a [SQLValue].
type SQLValueKind int

const (
	SQLValueString SQLValueKind = iota
	SQLValueInt
	SQLValueDouble
	SQLValueBool
	SQLValueBytes
	SQLValueNull
)

// BlobStoreDatastoreProvider is implemented by resource plugins that offer blob
// storage.
type BlobStoreDatastoreProvider interface {
	BlobGet(ctx context.Context, namespace, key string) (data []byte, contentType string, metadata map[string]string, err error)
	BlobPut(ctx context.Context, namespace, key string, data []byte, contentType string, metadata map[string]string) error
	BlobDelete(ctx context.Context, namespace, key string) error
	BlobList(ctx context.Context, namespace, prefix, cursor string, limit int32) (entries []BlobEntry, nextCursor string, err error)
}
