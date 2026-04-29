package indexeddb

import (
	"context"
	"errors"
)

var (
	ErrNotFound           = errors.New("datastore: not found")
	ErrAlreadyExists      = errors.New("datastore: already exists")
	ErrKeysOnly           = errors.New("datastore: value not available on key-only cursor")
	ErrReadOnly           = errors.New("datastore: transaction is readonly")
	ErrTransactionDone    = errors.New("datastore: transaction is already finished")
	ErrInvalidTransaction = errors.New("datastore: invalid transaction")
)

// CursorDirection controls the traversal order of a cursor.
type CursorDirection string

const (
	CursorNext       CursorDirection = "next"
	CursorNextUnique CursorDirection = "nextunique"
	CursorPrev       CursorDirection = "prev"
	CursorPrevUnique CursorDirection = "prevunique"
)

type Record = map[string]any

// TransactionMode controls whether a transaction may mutate scoped stores.
type TransactionMode string

const (
	TransactionReadonly  TransactionMode = "readonly"
	TransactionReadwrite TransactionMode = "readwrite"
)

// TransactionDurabilityHint mirrors the W3C IndexedDB durability option as a
// provider hint. It is not a portable durability guarantee.
type TransactionDurabilityHint string

const (
	TransactionDurabilityDefault TransactionDurabilityHint = "default"
	TransactionDurabilityStrict  TransactionDurabilityHint = "strict"
	TransactionDurabilityRelaxed TransactionDurabilityHint = "relaxed"
)

type TransactionOptions struct {
	DurabilityHint TransactionDurabilityHint
}

// Datastore is the IndexedDB-inspired interface every provider implements.
// Implementations must be safe for concurrent use.
type IndexedDB interface {
	ObjectStore(name string) ObjectStore
	Transaction(ctx context.Context, stores []string, mode TransactionMode, opts TransactionOptions) (Transaction, error)
	CreateObjectStore(ctx context.Context, name string, schema ObjectStoreSchema) error
	DeleteObjectStore(ctx context.Context, name string) error
	Ping(ctx context.Context) error
	Close() error
}

// Transaction is an explicit IndexedDB transaction over a fixed object-store
// scope. Cursor operations are intentionally excluded from the initial
// transaction contract.
type Transaction interface {
	ObjectStore(name string) TransactionObjectStore
	Commit(ctx context.Context) error
	Abort(ctx context.Context) error
}

// TransactionObjectStore provides transaction-scoped object-store operations.
type TransactionObjectStore interface {
	Get(ctx context.Context, id string) (Record, error)
	GetKey(ctx context.Context, id string) (string, error)
	Add(ctx context.Context, record Record) error
	Put(ctx context.Context, record Record) error
	Delete(ctx context.Context, id string) error
	Clear(ctx context.Context) error
	GetAll(ctx context.Context, r *KeyRange) ([]Record, error)
	GetAllKeys(ctx context.Context, r *KeyRange) ([]string, error)
	Count(ctx context.Context, r *KeyRange) (int64, error)
	DeleteRange(ctx context.Context, r KeyRange) (int64, error)
	Index(name string) TransactionIndex
}

// TransactionIndex provides transaction-scoped index operations.
type TransactionIndex interface {
	Get(ctx context.Context, values ...any) (Record, error)
	GetKey(ctx context.Context, values ...any) (string, error)
	GetAll(ctx context.Context, r *KeyRange, values ...any) ([]Record, error)
	GetAllKeys(ctx context.Context, r *KeyRange, values ...any) ([]string, error)
	Count(ctx context.Context, r *KeyRange, values ...any) (int64, error)
	Delete(ctx context.Context, values ...any) (int64, error)
}

// ObjectStore provides CRUD by primary key ("id" field) and index-based queries.
type ObjectStore interface {
	// Primary key CRUD
	Get(ctx context.Context, id string) (Record, error)
	GetKey(ctx context.Context, id string) (string, error)
	Add(ctx context.Context, record Record) error
	Put(ctx context.Context, record Record) error
	Delete(ctx context.Context, id string) error

	// Bulk operations. Optional KeyRange scopes to a subset of primary keys.
	Clear(ctx context.Context) error
	GetAll(ctx context.Context, r *KeyRange) ([]Record, error)
	GetAllKeys(ctx context.Context, r *KeyRange) ([]string, error)
	Count(ctx context.Context, r *KeyRange) (int64, error)
	DeleteRange(ctx context.Context, r KeyRange) (int64, error)

	// Index access
	Index(name string) Index

	// Cursor iteration
	OpenCursor(ctx context.Context, r *KeyRange, dir CursorDirection) (Cursor, error)
	OpenKeyCursor(ctx context.Context, r *KeyRange, dir CursorDirection) (Cursor, error)
}

// Index provides queries on a named index.
// Values correspond to the index's KeyPath fields in order.
type Index interface {
	Get(ctx context.Context, values ...any) (Record, error)
	GetKey(ctx context.Context, values ...any) (string, error)
	GetAll(ctx context.Context, r *KeyRange, values ...any) ([]Record, error)
	GetAllKeys(ctx context.Context, r *KeyRange, values ...any) ([]string, error)
	Count(ctx context.Context, r *KeyRange, values ...any) (int64, error)
	Delete(ctx context.Context, values ...any) (int64, error)

	// Cursor iteration
	OpenCursor(ctx context.Context, r *KeyRange, dir CursorDirection, values ...any) (Cursor, error)
	OpenKeyCursor(ctx context.Context, r *KeyRange, dir CursorDirection, values ...any) (Cursor, error)
}

// Cursor iterates over records in an object store or index.
// Modeled after IDBCursor / IDBCursorWithValue.
//
// Usage:
//
//	cursor, err := store.OpenCursor(ctx, nil, indexeddb.CursorNext)
//	if err != nil { ... }
//	defer cursor.Close()
//	for cursor.Continue() {
//	    rec, _ := cursor.Value()
//	    // ...
//	}
//	if err := cursor.Err(); err != nil { ... }
type Cursor interface {
	// Continue advances to the next record. Returns false when exhausted.
	Continue() bool
	// ContinueToKey advances to the next record whose key is >= the given key
	// (or <= for reverse cursors).
	ContinueToKey(key any) bool
	// Advance skips count records, then positions on the next one.
	Advance(count int) bool

	// Key returns the cursor's current key (index key for index cursors,
	// primary key for object store cursors).
	Key() any
	// PrimaryKey returns the primary key at the cursor's current position.
	PrimaryKey() string
	// Value returns the record at the cursor's current position.
	// Returns ErrKeysOnly if the cursor was opened via OpenKeyCursor.
	Value() (Record, error)

	// Delete removes the record at the cursor's current position.
	Delete() error
	// Update replaces the record at the cursor's current position.
	Update(value Record) error

	// Err returns any error encountered during iteration.
	Err() error
	// Close releases cursor resources. Must be called when done.
	Close() error
}

// KeyRange represents a range over keys, modeled after IDBKeyRange.
// A nil KeyRange means "all records".
type KeyRange struct {
	Lower     any
	Upper     any
	LowerOpen bool // true = exclusive lower bound (>), false = inclusive (>=)
	UpperOpen bool // true = exclusive upper bound (<), false = inclusive (<=)
}

func Only(value any) *KeyRange {
	return &KeyRange{Lower: value, Upper: value}
}

func LowerBound(value any, open bool) *KeyRange {
	return &KeyRange{Lower: value, LowerOpen: open}
}

func UpperBound(value any, open bool) *KeyRange {
	return &KeyRange{Upper: value, UpperOpen: open}
}

func Bound(lower, upper any, lowerOpen, upperOpen bool) *KeyRange {
	return &KeyRange{Lower: lower, Upper: upper, LowerOpen: lowerOpen, UpperOpen: upperOpen}
}
