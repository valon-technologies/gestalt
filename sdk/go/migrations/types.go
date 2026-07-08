// Package migrations runs declarative IndexedDB schema revisions with a ledger.
package migrations

import (
	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

const (
	defaultLedgerStore = "_gestalt_migrations"
	ledgerKeyColumn    = "revision_id"
	ledgerAppliedCol   = "applied_at"
)

// StoreDeclaration describes one object store to ensure exists.
type StoreDeclaration struct {
	Name    string
	Columns []indexeddb.ColumnDef
	Indexes []indexeddb.IndexSchema
}

// AddIndexDeclaration adds an index to an existing store.
type AddIndexDeclaration struct {
	Store string
	Index indexeddb.IndexSchema
}

// IndexRef identifies an index to drop.
type IndexRef struct {
	Store string
	Name  string
}

// SchemaDeclaration is a declarative schema delta.
type SchemaDeclaration struct {
	Stores      []StoreDeclaration
	AddIndexes  []AddIndexDeclaration
	DropStores  []string
	DropIndexes []IndexRef
}

// SchemaRevision applies a schema declaration.
type SchemaRevision struct {
	ID     string
	Schema SchemaDeclaration
}

// BackfillTransform copies rows from one store into another.
type BackfillTransform struct {
	From  string
	Into  string
	Value func(row indexeddb.Record) indexeddb.Record
}

// BackfillRevision runs a backfill transform.
type BackfillRevision struct {
	ID       string
	Backfill BackfillTransform
}

// Revision is one ordered migration step.
type Revision struct {
	ID       string
	Schema   *SchemaDeclaration
	Backfill *BackfillTransform
}

// RunOptions configures a migration run.
type RunOptions struct {
	Revisions   []Revision
	LedgerStore string
}

// Result reports which revisions were applied this run.
type Result struct {
	Applied []string
	Head    string
}

// MigrationError is returned when migrations cannot proceed.
type MigrationError struct {
	Message   string
	Current   string
	Attempted string
	Cause     error
}

func (e *MigrationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *MigrationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
