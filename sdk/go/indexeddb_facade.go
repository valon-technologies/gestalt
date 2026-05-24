package gestalt

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	rpcidb "github.com/valon-technologies/gestalt/server/rpc/indexeddb"
)

// IndexedDB data types (aliases to sdk/go/indexeddb).
type (
	Record                   = indexeddb.Record
	KeyRange                 = indexeddb.KeyRange
	CursorDirection          = indexeddb.CursorDirection
	TransactionMode          = indexeddb.TransactionMode
	TransactionDurabilityHint = indexeddb.TransactionDurabilityHint
	TransactionOptions       = indexeddb.TransactionOptions
	IndexSchema              = indexeddb.IndexSchema
	ColumnType               = indexeddb.ColumnType
	ColumnDef                = indexeddb.ColumnDef
	ObjectStoreSchema        = indexeddb.ObjectStoreSchema
	ObjectStoreOptions       = indexeddb.ObjectStoreOptions
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

// IndexedDB client capability interfaces (provider-side types keep IndexedDB* names in indexeddb_provider.go).
type (
	IndexedDBDatabase               = indexeddb.Database
	IndexedDBObjectStore          = indexeddb.ObjectStore
	IndexedDBIndex                  = indexeddb.Index
	IndexedDBClientTransaction      = indexeddb.Transaction
	IndexedDBTransactionObjectStore = indexeddb.TransactionObjectStore
	IndexedDBTransactionIndex       = indexeddb.TransactionIndex
	IndexedDBClientCursor             = indexeddb.Cursor
	IndexedDBRangeDeleter           = indexeddb.RangeDeleter
	IndexedDBMutableIndex           = indexeddb.MutableIndex
)

var indexedDBTransports sync.Map

// IndexedDB connects to the IndexedDB provider exposed by gestaltd.
func IndexedDB(ctx context.Context, name ...string) (indexeddb.Database, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target, token, err := host.Target("indexeddb")
	if err != nil {
		return nil, err
	}
	binding := ""
	if len(name) > 0 {
		binding = strings.TrimSpace(name[0])
	}
	transport := getIndexedDBTransport(binding)
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stub, err := host.ServiceClient(dialCtx, "indexeddb", target, token, binding, transport, proto.NewIndexedDBClient)
	if err != nil {
		return nil, fmt.Errorf("indexeddb: connect to host: %w", err)
	}
	return rpcidb.NewClient(stub, rpcidb.Options{}), nil
}

func getIndexedDBTransport(binding string) *host.SharedTransport[proto.IndexedDBClient] {
	val, _ := indexedDBTransports.LoadOrStore(binding, &host.SharedTransport[proto.IndexedDBClient]{})
	return val.(*host.SharedTransport[proto.IndexedDBClient])
}
