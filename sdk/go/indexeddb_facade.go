package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	idbhost "github.com/valon-technologies/gestalt/sdk/go/indexeddb/host"
)

// IndexedDB data types (aliases to sdk/go/indexeddb).
type (
	Record                    = indexeddb.Record
	KeyRange                  = indexeddb.KeyRange
	CursorDirection           = indexeddb.CursorDirection
	TransactionMode           = indexeddb.TransactionMode
	TransactionDurabilityHint = indexeddb.TransactionDurabilityHint
	TransactionOptions        = indexeddb.TransactionOptions
	IndexSchema               = indexeddb.IndexSchema
	ColumnType                = indexeddb.ColumnType
	ColumnDef                 = indexeddb.ColumnDef
	ObjectStoreSchema         = indexeddb.ObjectStoreSchema
	ObjectStoreOptions        = indexeddb.ObjectStoreOptions
)

const (
	CursorNext       = indexeddb.CursorNext
	CursorNextUnique = indexeddb.CursorNextUnique
	CursorPrev       = indexeddb.CursorPrev
	CursorPrevUnique = indexeddb.CursorPrevUnique

	TransactionReadonly  = indexeddb.TransactionReadonly
	TransactionReadwrite = indexeddb.TransactionReadwrite

	TransactionDurabilityDefault = indexeddb.TransactionDurabilityDefault
	TransactionDurabilityStrict  = indexeddb.TransactionDurabilityStrict
	TransactionDurabilityRelaxed = indexeddb.TransactionDurabilityRelaxed

	TypeString = indexeddb.TypeString
	TypeInt    = indexeddb.TypeInt
	TypeFloat  = indexeddb.TypeFloat
	TypeBool   = indexeddb.TypeBool
	TypeTime   = indexeddb.TypeTime
	TypeBytes  = indexeddb.TypeBytes
	TypeJSON   = indexeddb.TypeJSON
)

var (
	ErrNotFound           = indexeddb.ErrNotFound
	ErrAlreadyExists      = indexeddb.ErrAlreadyExists
	ErrKeysOnly           = indexeddb.ErrKeysOnly
	ErrTransactionDone    = indexeddb.ErrTransactionDone
	ErrReadOnly           = indexeddb.ErrReadOnly
	ErrInvalidTransaction = indexeddb.ErrInvalidTransaction
)

// IndexedDB capability interfaces (provider-side types keep IndexedDB* names in indexeddb_provider.go).
type (
	IndexedDBDatabase               = indexeddb.Database
	IndexedDBObjectStore            = indexeddb.ObjectStore
	IndexedDBIndex                  = indexeddb.Index
	IndexedDBTransactionObjectStore = indexeddb.TransactionObjectStore
	IndexedDBTransactionIndex       = indexeddb.TransactionIndex
)

// IndexedDB connects to the IndexedDB provider exposed by gestaltd.
func IndexedDB(ctx context.Context, name ...string) (indexeddb.Database, error) {
	opts := idbhost.OpenOptions{}
	if len(name) > 0 {
		opts.Binding = name[0]
	}
	return idbhost.Open(ctx, opts)
}
