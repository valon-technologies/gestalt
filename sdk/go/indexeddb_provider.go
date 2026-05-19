package gestalt

import "context"

// IndexedDBProvider is implemented by providers that expose IndexedDB-style
// factory lifecycle semantics: named databases, versioned open, upgrade
// callbacks, deletion, and scoped database connections.
type IndexedDBProvider interface {
	Provider
	OpenDatabase(ctx context.Context, name string, opts OpenOptions) (IDBDatabase, error)
	OpenCurrentDatabase(ctx context.Context, name string, opts OpenOptions) (IDBDatabase, error)
	DeleteDatabase(ctx context.Context, name string, opts DeleteOptions) (DeleteDatabaseResult, error)
	Databases(ctx context.Context) ([]IDBDatabaseInfo, error)
	CompareKeys(ctx context.Context, first any, second any) (int, error)
}

// IDBDatabase is an opened database connection returned by an
// [IndexedDBProvider]. Store, index, cursor, and transaction operations
// are scoped to this connection by the SDK transport adapter.
type IDBDatabase interface {
	Name() string
	Version() uint64
	ObjectStoreNames(ctx context.Context) ([]string, error)
	IndexedDBOperations
	Close() error
}

type IndexedDBOperations interface {
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

	OpenCursor(ctx context.Context, req IndexedDBOpenCursorRequest) (IDBCursor, error)
	BeginTransaction(ctx context.Context, req IndexedDBBeginTransactionRequest) (IDBTransaction, error)
}

type IndexedDBObjectStoreRequest struct {
	Store string
	ID    string
}

type IndexedDBRecordRequest struct {
	Store  string
	Record Record
}

type IndexedDBObjectStoreRangeRequest struct {
	Store string
	Range *KeyRange
}

type IndexedDBIndexQueryRequest struct {
	Store  string
	Index  string
	Values []any
	Range  *KeyRange
}

type IndexedDBOpenCursorRequest struct {
	Store     string
	Range     *KeyRange
	Direction CursorDirection
	KeysOnly  bool
	Index     string
	Values    []any
}

// IDBCursorEntry is one provider-side cursor position.
type IDBCursorEntry struct {
	Key        any
	PrimaryKey string
	Record     Record
}

// IDBCursor is the runtime object returned from OpenCursor. Returning a
// nil entry from movement methods indicates cursor exhaustion.
type IDBCursor interface {
	Next(ctx context.Context) (*IDBCursorEntry, error)
	ContinueToKey(ctx context.Context, key any) (*IDBCursorEntry, error)
	Advance(ctx context.Context, count int) (*IDBCursorEntry, error)
	Delete(ctx context.Context) error
	Update(ctx context.Context, record Record) (*IDBCursorEntry, error)
	Close() error
}

type IndexedDBBeginTransactionRequest struct {
	Stores         []string
	Mode           TransactionMode
	DurabilityHint TransactionDurabilityHint
}

// IDBTransaction is a provider-side transaction scoped to object stores.
type IDBTransaction interface {
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
