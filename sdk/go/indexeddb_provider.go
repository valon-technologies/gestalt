package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/client"
)

// IndexedDBProvider is implemented by providers that serve the IndexedDB
// surface. The SDK owns the gRPC/protobuf transport adapter; provider code
// implements this typed interface instead of importing generated protobuf
// bindings.
type IndexedDBProvider interface {
	Provider
	CreateObjectStore(ctx context.Context, name string, schema ObjectStoreOptions) error
	DeleteObjectStore(ctx context.Context, name string) error

	Get(ctx context.Context, req IndexedDBObjectStoreRequest) (Record, error)
	GetKey(ctx context.Context, req IndexedDBObjectStoreRequest) (string, error)
	Add(ctx context.Context, req IndexedDBRecordRequest) error
	Put(ctx context.Context, req IndexedDBRecordRequest) error
	Delete(ctx context.Context, req IndexedDBObjectStoreRequest) error
	Clear(ctx context.Context, store string) error
	GetAll(ctx context.Context, req IndexedDBObjectStoreRangeRequest) ([]Record, error)
	GetAllKeys(ctx context.Context, req IndexedDBObjectStoreRangeRequest) ([]string, error)
	Count(ctx context.Context, req IndexedDBObjectStoreRangeRequest) (int64, error)
	DeleteRange(ctx context.Context, req IndexedDBObjectStoreRangeRequest) (int64, error)

	IndexGet(ctx context.Context, req IndexedDBIndexQueryRequest) (Record, error)
	IndexGetKey(ctx context.Context, req IndexedDBIndexQueryRequest) (string, error)
	IndexGetAll(ctx context.Context, req IndexedDBIndexQueryRequest) ([]Record, error)
	IndexGetAllKeys(ctx context.Context, req IndexedDBIndexQueryRequest) ([]string, error)
	IndexCount(ctx context.Context, req IndexedDBIndexQueryRequest) (int64, error)
	IndexDelete(ctx context.Context, req IndexedDBIndexQueryRequest) (int64, error)

	OpenCursor(ctx context.Context, req IndexedDBOpenCursorRequest) (IndexedDBCursor, error)
	BeginTransaction(ctx context.Context, req IndexedDBBeginTransactionRequest) (IndexedDBTransaction, error)
}

// IndexedDBIndexProvider is an optional capability implemented by providers that
// support adding or removing secondary indexes on an existing object store, in
// addition to [IndexedDBProvider]. The SDK returns Unimplemented for providers
// that do not implement it.
type IndexedDBIndexProvider interface {
	CreateIndex(ctx context.Context, req IndexedDBCreateIndexRequest) error
	DeleteIndex(ctx context.Context, req IndexedDBDeleteIndexRequest) error
}

// IndexedDBCreateIndexRequest adds a secondary index to an existing store.
type IndexedDBCreateIndexRequest struct {
	Store   string
	Name    string
	KeyPath []string
	Unique  bool
}

// IndexedDBDeleteIndexRequest removes a secondary index from an existing store.
type IndexedDBDeleteIndexRequest struct {
	Store string
	Name  string
}

// IndexedDBObjectStoreRequest names a store within a database.
type IndexedDBObjectStoreRequest struct {
	Store string
	ID    string
}

// IndexedDBRecordRequest addresses one record by key.
type IndexedDBRecordRequest struct {
	Store  string
	Record Record
}

// IndexedDBObjectStoreRangeRequest addresses a query over a store.
type IndexedDBObjectStoreRangeRequest struct {
	Store string
	Query *client.IndexedDBQuery
	Count *uint32
}

// IndexedDBIndexQueryRequest queries records through a secondary index.
type IndexedDBIndexQueryRequest struct {
	Store string
	Index string
	Query *client.IndexedDBQuery
	Count *uint32
}

// IndexedDBOpenCursorRequest opens a cursor over a store or index.
type IndexedDBOpenCursorRequest struct {
	Store     string
	Index     string
	Query     *client.IndexedDBQuery
	Direction CursorDirection
	KeysOnly  bool
}

// IndexedDBCursorEntry is one record yielded by a cursor.
type IndexedDBCursorEntry struct {
	Key        any
	PrimaryKey string
	Record     Record
}

// IndexedDBCursor is the runtime object returned from OpenCursor. Returning a
// nil entry from movement methods indicates cursor exhaustion.
type IndexedDBCursor interface {
	Next(ctx context.Context) (*IndexedDBCursorEntry, error)
	ContinueToKey(ctx context.Context, key any) (*IndexedDBCursorEntry, error)
	Advance(ctx context.Context, count int) (*IndexedDBCursorEntry, error)
	Delete(ctx context.Context) error
	Update(ctx context.Context, record Record) (*IndexedDBCursorEntry, error)
	Close() error
}

// IndexedDBBeginTransactionRequest begins a transaction over named stores.
type IndexedDBBeginTransactionRequest struct {
	Stores         []string
	Mode           TransactionMode
	DurabilityHint TransactionDurabilityHint
}

// IndexedDBTransaction is one open transaction handle.
type IndexedDBTransaction interface {
	Commit(ctx context.Context) error
	Abort(ctx context.Context) error
	Get(ctx context.Context, req IndexedDBObjectStoreRequest) (Record, error)
	GetKey(ctx context.Context, req IndexedDBObjectStoreRequest) (string, error)
	Add(ctx context.Context, req IndexedDBRecordRequest) error
	Put(ctx context.Context, req IndexedDBRecordRequest) error
	Delete(ctx context.Context, req IndexedDBObjectStoreRequest) error
	Clear(ctx context.Context, store string) error
	GetAll(ctx context.Context, req IndexedDBObjectStoreRangeRequest) ([]Record, error)
	GetAllKeys(ctx context.Context, req IndexedDBObjectStoreRangeRequest) ([]string, error)
	Count(ctx context.Context, req IndexedDBObjectStoreRangeRequest) (int64, error)
	DeleteRange(ctx context.Context, req IndexedDBObjectStoreRangeRequest) (int64, error)
	IndexGet(ctx context.Context, req IndexedDBIndexQueryRequest) (Record, error)
	IndexGetKey(ctx context.Context, req IndexedDBIndexQueryRequest) (string, error)
	IndexGetAll(ctx context.Context, req IndexedDBIndexQueryRequest) ([]Record, error)
	IndexGetAllKeys(ctx context.Context, req IndexedDBIndexQueryRequest) ([]string, error)
	IndexCount(ctx context.Context, req IndexedDBIndexQueryRequest) (int64, error)
	IndexDelete(ctx context.Context, req IndexedDBIndexQueryRequest) (int64, error)
}
