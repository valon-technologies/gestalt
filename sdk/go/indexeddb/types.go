package indexeddb

// Record is the JSON-like value stored in an object store row.
type Record = map[string]any

// CursorDirection controls IndexedDB cursor traversal order (maps to IDBCursorDirection).
type CursorDirection string

const (
	CursorNext       CursorDirection = "next"
	CursorNextUnique CursorDirection = "nextunique"
	CursorPrev       CursorDirection = "prev"
	CursorPrevUnique CursorDirection = "prevunique"
)

// TransactionMode controls whether a transaction may mutate scoped stores.
type TransactionMode string

const (
	TransactionReadonly  TransactionMode = "readonly"
	TransactionReadwrite TransactionMode = "readwrite"
	// TransactionVersionChange names the W3C mode; not supported by the Gestalt protocol yet.
	TransactionVersionChange TransactionMode = "versionchange"
)

// TransactionDurabilityHint mirrors the W3C IndexedDB durability option as a provider hint.
type TransactionDurabilityHint string

const (
	TransactionDurabilityDefault TransactionDurabilityHint = "default"
	TransactionDurabilityStrict  TransactionDurabilityHint = "strict"
	TransactionDurabilityRelaxed TransactionDurabilityHint = "relaxed"
)

type TransactionOptions struct {
	DurabilityHint TransactionDurabilityHint
}

// IndexSchema describes one secondary index on an object store.
type IndexSchema struct {
	Name    string
	KeyPath []string
	Unique  bool
}

// ColumnType describes a provider-preserved scalar column type.
type ColumnType int32

const (
	TypeString ColumnType = iota
	TypeInt
	TypeFloat
	TypeBool
	TypeTime
	TypeBytes
	TypeJSON
)

// ColumnDef describes one provider-preserved object-store column.
type ColumnDef struct {
	Name       string
	Type       ColumnType
	PrimaryKey bool
	NotNull    bool
	Unique     bool
}

// ObjectStoreOptions describes indexes and columns for CreateObjectStore.
type ObjectStoreOptions struct {
	Indexes []IndexSchema
	Columns []ColumnDef
}

// KeyRange represents a range over keys (maps to IDBKeyRange).
type KeyRange struct {
	Lower     any
	Upper     any
	LowerOpen bool
	UpperOpen bool
}
