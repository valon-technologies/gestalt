package indexeddb

import "errors"

// Sentinel errors store implementations return so the daemon can map
// database failures to canonical statuses.
var (
	ErrNotFound           = errors.New("indexeddb: not found")
	ErrAlreadyExists      = errors.New("indexeddb: already exists")
	ErrKeysOnly           = errors.New("indexeddb: value not available on key-only cursor")
	ErrTransactionDone    = errors.New("indexeddb: transaction is already finished")
	ErrReadOnly           = errors.New("indexeddb: transaction is readonly")
	ErrInvalidTransaction = errors.New("indexeddb: invalid transaction")
	ErrUnsupported        = errors.New("indexeddb: unsupported")
)
