package gestalt

import "context"

// IndexedDBProvider is implemented by providers that serve an IndexedDB-style
// datastore. It accepts both the authored SDK surface and the legacy generated
// gRPC server surface while providers migrate.
type IndexedDBProvider interface {
	Provider
}

// AuthoredIndexedDBProvider is the preferred provider surface. The SDK owns the
// gRPC/protobuf transport adapter; provider code should implement this
// interface instead of importing generated protobuf bindings.
type AuthoredIndexedDBProvider interface {
	IndexedDBProvider
	CreateObjectStore(ctx context.Context, name string, schema ObjectStoreSchema) error
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

type IndexedDBBeginTransactionRequest struct {
	Stores         []string
	Mode           TransactionMode
	DurabilityHint TransactionDurabilityHint
}

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
