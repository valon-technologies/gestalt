package datastore

import (
	"context"
	"errors"
)

var (
	ErrNotFound      = errors.New("datastore: not found")
	ErrAlreadyExists = errors.New("datastore: already exists")
)

type Record = map[string]any

// Datastore is the IndexedDB-inspired interface every provider implements.
// Implementations must be safe for concurrent use.
type Datastore interface {
	ObjectStore(name string) ObjectStore
	CreateObjectStore(ctx context.Context, name string, schema ObjectStoreSchema) error
	DeleteObjectStore(ctx context.Context, name string) error
	Ping(ctx context.Context) error
	Close() error
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
