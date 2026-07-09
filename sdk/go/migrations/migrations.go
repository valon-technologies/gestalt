package migrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

// Run brings the database up to the declared head revision.
func Run(ctx context.Context, db indexeddb.Database, opts RunOptions) (Result, error) {
	revisions, err := validateRevisions(opts.Revisions)
	if err != nil {
		return Result{}, err
	}
	if len(revisions) == 0 {
		return Result{Applied: nil, Head: ""}, nil
	}

	ledgerStore := strings.TrimSpace(opts.LedgerStore)
	if ledgerStore == "" {
		ledgerStore = defaultLedgerStore
	}
	if err := ensureLedgerStore(ctx, db, ledgerStore); err != nil {
		return Result{}, err
	}

	appliedKeys, err := db.ObjectStore(ledgerStore).GetAllKeys(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	applied := make(map[string]struct{}, len(appliedKeys))
	for _, id := range appliedKeys {
		applied[id] = struct{}{}
	}

	if err := assertNotAheadOfCode(revisions, applied); err != nil {
		return Result{}, err
	}
	if err := assertContiguousPrefix(revisions, applied); err != nil {
		return Result{}, err
	}

	attemptedHead := revisions[len(revisions)-1].ID
	current := latestAppliedID(revisions, applied)
	appliedNow := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		if _, ok := applied[revision.ID]; ok {
			continue
		}
		if err := applyRevision(ctx, db, revision); err != nil {
			var migErr *MigrationError
			if errors.As(err, &migErr) {
				return Result{}, err
			}
			return Result{}, &MigrationError{
				Message:   fmt.Sprintf("migration %s failed: %s", jsonString(revision.ID), errorText(err)),
				Current:   current,
				Attempted: attemptedHead,
				Cause:     err,
			}
		}
		if err := recordRevision(ctx, db, ledgerStore, revision.ID); err != nil {
			var migErr *MigrationError
			if errors.As(err, &migErr) {
				return Result{}, err
			}
			return Result{}, &MigrationError{
				Message:   fmt.Sprintf("migration %s failed: %s", jsonString(revision.ID), errorText(err)),
				Current:   current,
				Attempted: attemptedHead,
				Cause:     err,
			}
		}
		appliedNow = append(appliedNow, revision.ID)
		current = revision.ID
	}
	return Result{Applied: appliedNow, Head: attemptedHead}, nil
}

func validateRevisions(revisions []Revision) ([]Revision, error) {
	seen := make(map[string]struct{}, len(revisions))
	out := make([]Revision, 0, len(revisions))
	for _, revision := range revisions {
		id := strings.TrimSpace(revision.ID)
		if id == "" {
			return nil, &MigrationError{Message: "every revision needs a non-empty id"}
		}
		if _, ok := seen[id]; ok {
			return nil, &MigrationError{Message: fmt.Sprintf("duplicate revision id %s", jsonString(id))}
		}
		seen[id] = struct{}{}

		hasSchema := revision.Schema != nil
		hasBackfill := revision.Backfill != nil
		if hasSchema == hasBackfill {
			return nil, &MigrationError{
				Message: fmt.Sprintf(
					`revision %s must declare exactly one of "schema" or "backfill"`,
					jsonString(id),
				),
			}
		}
		if revision.Backfill != nil && revision.Backfill.From == revision.Backfill.Into {
			return nil, &MigrationError{
				Message: fmt.Sprintf(
					`revision %s backfill "from" and "into" must differ: a backfill reads an immutable source and writes a distinct target, so it cannot read its own output and is idempotent by construction`,
					jsonString(id),
				),
			}
		}
		copied := revision
		copied.ID = id
		out = append(out, copied)
	}
	return out, nil
}

func revisionNamespaces(revisions []Revision) (prefixes map[string]struct{}, hasFlat bool) {
	prefixes = make(map[string]struct{}, len(revisions))
	for _, revision := range revisions {
		id := strings.TrimSpace(revision.ID)
		if id == "" {
			continue
		}
		if i := strings.LastIndex(id, "/"); i >= 0 {
			prefixes[id[:i+1]] = struct{}{}
			continue
		}
		hasFlat = true
	}
	return prefixes, hasFlat
}

func ledgerIDDirectoryPrefix(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[:i+1]
	}
	return ""
}

func ledgerIDOwnedByProvider(id string, prefixes map[string]struct{}, hasFlat bool) bool {
	if strings.Contains(id, "/") {
		_, ok := prefixes[ledgerIDDirectoryPrefix(id)]
		return ok
	}
	return hasFlat && len(prefixes) == 0
}

func assertNotAheadOfCode(revisions []Revision, applied map[string]struct{}) error {
	declared := make(map[string]struct{}, len(revisions))
	for _, revision := range revisions {
		declared[revision.ID] = struct{}{}
	}
	prefixes, hasFlat := revisionNamespaces(revisions)
	var unknown []string
	for id := range applied {
		if _, ok := declared[id]; ok {
			continue
		}
		if !ledgerIDOwnedByProvider(id, prefixes, hasFlat) {
			continue
		}
		unknown = append(unknown, id)
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	attemptedHead := revisions[len(revisions)-1].ID
	return &MigrationError{
		Message: fmt.Sprintf(
			`ledger is ahead of code: applied revision(s) %s are not declared by this binary. Roll forward to a binary that defines them, or manually undo them and delete their ledger rows.`,
			joinJSONStrings(unknown),
		),
		Current:   unknown[len(unknown)-1],
		Attempted: attemptedHead,
	}
}

func assertContiguousPrefix(revisions []Revision, applied map[string]struct{}) error {
	sawUnapplied := false
	var outOfOrder []string
	for _, revision := range revisions {
		if _, ok := applied[revision.ID]; ok {
			if sawUnapplied {
				outOfOrder = append(outOfOrder, revision.ID)
			}
		} else {
			sawUnapplied = true
		}
	}
	if len(outOfOrder) == 0 {
		return nil
	}
	attemptedHead := ""
	if len(revisions) > 0 {
		attemptedHead = revisions[len(revisions)-1].ID
	}
	return &MigrationError{
		Message: fmt.Sprintf(
			`ledger has gaps: revision(s) %s are applied but an earlier declared revision is not. Revisions are an append-only ledger — a new revision must be added after all applied ones, never inserted or reordered before them.`,
			joinJSONStrings(outOfOrder),
		),
		Attempted: attemptedHead,
	}
}

func latestAppliedID(revisions []Revision, applied map[string]struct{}) string {
	var current string
	for _, revision := range revisions {
		if _, ok := applied[revision.ID]; ok {
			current = revision.ID
		}
	}
	return current
}

func ensureLedgerStore(ctx context.Context, db indexeddb.Database, ledgerStore string) error {
	return createStoreIfAbsent(ctx, db, StoreDeclaration{
		Name: ledgerStore,
		Columns: []indexeddb.ColumnDef{
			{Name: ledgerKeyColumn, PrimaryKey: true, NotNull: true},
			{Name: ledgerAppliedCol, NotNull: true},
		},
	})
}

func applyRevision(ctx context.Context, db indexeddb.Database, revision Revision) error {
	if isSchemaRevision(revision) {
		return applySchema(ctx, db, *revision.Schema)
	}
	return applyBackfill(ctx, db, *revision.Backfill)
}

func applyBackfill(ctx context.Context, db indexeddb.Database, transform BackfillTransform) error {
	target := db.ObjectStore(transform.Into)
	cursor, err := db.ObjectStore(transform.From).OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		return err
	}
	defer cursor.Close()

	for cursor.Continue() {
		row, err := cursor.Value()
		if err != nil {
			if errors.Is(err, indexeddb.ErrNotFound) {
				continue
			}
			return err
		}
		if row == nil {
			continue
		}
		if err := target.Put(ctx, transform.Value(row)); err != nil {
			return err
		}
	}
	return cursor.Err()
}

func applySchema(ctx context.Context, db indexeddb.Database, schema SchemaDeclaration) error {
	for _, store := range schema.Stores {
		if err := createStoreIfAbsent(ctx, db, store); err != nil {
			return err
		}
	}
	for _, entry := range schema.AddIndexes {
		if err := createIndexIfAbsent(ctx, db, entry.Store, entry.Index); err != nil {
			return err
		}
	}
	for _, entry := range schema.DropIndexes {
		if err := dropIndexIfPresent(ctx, db, entry.Store, entry.Name); err != nil {
			return err
		}
	}
	for _, name := range schema.DropStores {
		if err := dropStoreIfPresent(ctx, db, name); err != nil {
			return err
		}
	}
	return nil
}

func createStoreIfAbsent(ctx context.Context, db indexeddb.Database, store StoreDeclaration) error {
	opts := indexeddb.ObjectStoreOptions{}
	if store.Columns != nil {
		opts.Columns = store.Columns
	}
	if store.Indexes != nil {
		opts.Indexes = store.Indexes
	}
	_, err := db.CreateObjectStore(ctx, store.Name, opts)
	if err != nil && !errors.Is(err, indexeddb.ErrAlreadyExists) {
		return err
	}
	return nil
}

func createIndexIfAbsent(ctx context.Context, db indexeddb.Database, store string, index indexeddb.IndexSchema) error {
	idxMgr, ok := db.(indexeddb.IndexManager)
	if !ok {
		return fmt.Errorf("indexeddb: create index %q on %q: index manager not supported", index.Name, store)
	}
	err := idxMgr.CreateIndex(ctx, store, indexeddb.IndexDefinition{
		Name:    index.Name,
		KeyPath: index.KeyPath,
		Unique:  index.Unique,
	})
	if err != nil && !errors.Is(err, indexeddb.ErrAlreadyExists) {
		return err
	}
	return nil
}

func dropStoreIfPresent(ctx context.Context, db indexeddb.Database, name string) error {
	err := db.DeleteObjectStore(ctx, name)
	if err != nil && !errors.Is(err, indexeddb.ErrNotFound) {
		return err
	}
	return nil
}

func dropIndexIfPresent(ctx context.Context, db indexeddb.Database, store, name string) error {
	idxMgr, ok := db.(indexeddb.IndexManager)
	if !ok {
		return fmt.Errorf("indexeddb: delete index %q on %q: index manager not supported", name, store)
	}
	err := idxMgr.DeleteIndex(ctx, store, name)
	if err != nil && !errors.Is(err, indexeddb.ErrNotFound) {
		return err
	}
	return nil
}

func recordRevision(ctx context.Context, db indexeddb.Database, ledgerStore, id string) error {
	record := indexeddb.Record{
		ledgerKeyColumn:  id,
		ledgerAppliedCol: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if ledgerKeyColumn != "id" {
		record["id"] = id
	}
	return db.ObjectStore(ledgerStore).Put(ctx, record)
}

func isSchemaRevision(revision Revision) bool {
	return revision.Schema != nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func jsonString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%q", v)
	}
	return string(b)
}

func joinJSONStrings(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = jsonString(v)
	}
	return strings.Join(parts, ", ")
}
