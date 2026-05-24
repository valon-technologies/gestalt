package indexeddb

import (
	"context"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

type (
	Record                   = idb.Record
	KeyRange                 = idb.KeyRange
	CursorDirection          = idb.CursorDirection
	TransactionMode          = idb.TransactionMode
	TransactionDurabilityHint = idb.TransactionDurabilityHint
	TransactionOptions       = idb.TransactionOptions
	IndexSchema              = idb.IndexSchema
	ColumnType               = idb.ColumnType
	ColumnDef                = idb.ColumnDef
	ObjectStoreSchema        = idb.ObjectStoreSchema
	ObjectStoreOptions       = idb.ObjectStoreOptions
)

const (
	CursorNext       = idb.CursorNext
	CursorNextUnique = idb.CursorNextUnique
	CursorPrev       = idb.CursorPrev
	CursorPrevUnique = idb.CursorPrevUnique

	TransactionReadonly  = idb.TransactionReadonly
	TransactionReadwrite = idb.TransactionReadwrite

	TransactionDurabilityDefault = idb.TransactionDurabilityDefault
	TransactionDurabilityStrict  = idb.TransactionDurabilityStrict
	TransactionDurabilityRelaxed = idb.TransactionDurabilityRelaxed

	TypeString = idb.TypeString
	TypeInt    = idb.TypeInt
	TypeFloat  = idb.TypeFloat
	TypeBool   = idb.TypeBool
	TypeTime   = idb.TypeTime
	TypeBytes  = idb.TypeBytes
	TypeJSON   = idb.TypeJSON
)

var (
	ErrNotFound           = idb.ErrNotFound
	ErrAlreadyExists      = idb.ErrAlreadyExists
	ErrKeysOnly           = idb.ErrKeysOnly
	ErrReadOnly           = idb.ErrReadOnly
	ErrTransactionDone    = idb.ErrTransactionDone
	ErrInvalidTransaction = idb.ErrInvalidTransaction
)

// IndexedDB is the database capability interface plus server health checks.
type IndexedDB interface {
	idb.Database
	Pinger
}

type ObjectStore = idb.ObjectStore
type Index = idb.Index
type Transaction = idb.Transaction
type TransactionObjectStore = idb.TransactionObjectStore
type TransactionIndex = idb.TransactionIndex
type Cursor = idb.Cursor

type RangeDeleter = idb.RangeDeleter
type MutableIndex = idb.MutableIndex

// Pinger is server-local health checking.
type Pinger interface {
	Ping(ctx context.Context) error
}
