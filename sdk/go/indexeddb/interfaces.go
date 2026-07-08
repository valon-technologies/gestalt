package indexeddb

import (
	"context"
)

// Database maps to the W3C IDBDatabase surface (with documented Gestalt deviations).
type Database interface {
	CreateObjectStore(ctx context.Context, name string, opts ObjectStoreOptions) (ObjectStore, error)
	DeleteObjectStore(ctx context.Context, name string) error
	Transaction(ctx context.Context, stores []string, mode TransactionMode, opts TransactionOptions) (Transaction, error)
	// ObjectStore returns a handle using Gestalt auto-transaction semantics (non-W3C convenience).
	ObjectStore(name string) ObjectStore
	Close() error
}

// ObjectStore maps to IDBObjectStore for the supported Gestalt protocol subset.
type ObjectStore interface {
	Add(ctx context.Context, record Record) error
	Put(ctx context.Context, record Record) error
	Get(ctx context.Context, id string) (Record, error)
	GetKey(ctx context.Context, id string) (string, error)
	Delete(ctx context.Context, id string) error
	Clear(ctx context.Context) error
	GetAll(ctx context.Context, query any, count ...uint32) ([]Record, error)
	GetAllKeys(ctx context.Context, query any, count ...uint32) ([]string, error)
	Count(ctx context.Context, query any) (int64, error)
	DeleteRange(ctx context.Context, query any) (int64, error)
	Index(name string) Index
	OpenCursor(ctx context.Context, query any, dir CursorDirection) (Cursor, error)
	OpenKeyCursor(ctx context.Context, query any, dir CursorDirection) (Cursor, error)
}

// Index maps to IDBIndex for the supported Gestalt protocol subset.
type Index interface {
	Get(ctx context.Context, query any) (Record, error)
	GetKey(ctx context.Context, query any) (string, error)
	GetAll(ctx context.Context, query any, count ...uint32) ([]Record, error)
	GetAllKeys(ctx context.Context, query any, count ...uint32) ([]string, error)
	Count(ctx context.Context, query any) (int64, error)
	Delete(ctx context.Context, query any) (int64, error)
	OpenCursor(ctx context.Context, query any, dir CursorDirection) (Cursor, error)
	OpenKeyCursor(ctx context.Context, query any, dir CursorDirection) (Cursor, error)
}

// Transaction maps to IDBTransaction (cursor ops remain on stores/indexes).
type Transaction interface {
	ObjectStore(name string) TransactionObjectStore
	Commit(ctx context.Context) error
	Abort(ctx context.Context) error
}

// TransactionObjectStore provides transaction-scoped object store operations.
type TransactionObjectStore interface {
	Get(ctx context.Context, id string) (Record, error)
	GetKey(ctx context.Context, id string) (string, error)
	Add(ctx context.Context, record Record) error
	Put(ctx context.Context, record Record) error
	Delete(ctx context.Context, id string) error
	Clear(ctx context.Context) error
	GetAll(ctx context.Context, query any, count ...uint32) ([]Record, error)
	GetAllKeys(ctx context.Context, query any, count ...uint32) ([]string, error)
	Count(ctx context.Context, query any) (int64, error)
	DeleteRange(ctx context.Context, query any) (int64, error)
	Index(name string) TransactionIndex
}

// TransactionIndex provides transaction-scoped index operations.
type TransactionIndex interface {
	Get(ctx context.Context, query any) (Record, error)
	GetKey(ctx context.Context, query any) (string, error)
	GetAll(ctx context.Context, query any, count ...uint32) ([]Record, error)
	GetAllKeys(ctx context.Context, query any, count ...uint32) ([]string, error)
	Count(ctx context.Context, query any) (int64, error)
	Delete(ctx context.Context, query any) (int64, error)
}

// Cursor maps to IDBCursor / IDBCursorWithValue.
type Cursor interface {
	Continue() bool
	ContinueToKey(key any) bool
	Advance(count int) bool
	Key() any
	PrimaryKey() string
	Value() (Record, error)
	Delete() error
	Update(value Record) error
	Err() error
	Close() error
}

// Pinger is a server-side health extension; host clients may omit it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// IndexManager is an optional capability implemented by backends that support
// adding or removing secondary indexes on an existing object store.
type IndexManager interface {
	CreateIndex(ctx context.Context, store string, index IndexDefinition) error
	DeleteIndex(ctx context.Context, store, name string) error
}

// IndexDefinition describes a secondary index to create on an object store.
type IndexDefinition struct {
	Name    string
	KeyPath []string
	Unique  bool
}
