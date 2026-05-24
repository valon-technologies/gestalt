package coretesting

import (
	"context"
	"sort"
	"sync"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

type stubObjectStore struct {
	db      *StubIndexedDB
	mu      sync.RWMutex
	records map[string]idb.Record
	schema  idb.ObjectStoreSchema
}

func (o *stubObjectStore) clone(db *StubIndexedDB) *stubObjectStore {
	o.mu.RLock()
	defer o.mu.RUnlock()
	records := make(map[string]idb.Record, len(o.records))
	for id, record := range o.records {
		records[id] = cloneRecord(record)
	}
	return &stubObjectStore{
		db:      db,
		records: records,
		schema:  o.schema,
	}
}

func (o *stubObjectStore) readSchedule() func() {
	if o.db == nil {
		return func() {}
	}
	o.db.txMu.RLock()
	return o.db.txMu.RUnlock
}

func (o *stubObjectStore) writeSchedule() func() {
	if o.db == nil {
		return func() {}
	}
	o.db.txMu.Lock()
	return o.db.txMu.Unlock
}

func (o *stubObjectStore) Get(_ context.Context, id string) (idb.Record, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	o.mu.RLock()
	defer o.mu.RUnlock()
	r, ok := o.records[id]
	if !ok {
		return nil, idb.ErrNotFound
	}
	return r, nil
}

func (o *stubObjectStore) GetKey(_ context.Context, id string) (string, error) {
	if o.db.Err != nil {
		return "", o.db.Err
	}
	done := o.readSchedule()
	defer done()
	o.mu.RLock()
	defer o.mu.RUnlock()
	if _, ok := o.records[id]; !ok {
		return "", idb.ErrNotFound
	}
	return id, nil
}

func (o *stubObjectStore) Add(_ context.Context, record idb.Record) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	done := o.writeSchedule()
	defer done()
	id, _ := record["id"].(string)
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.records[id]; ok {
		return idb.ErrAlreadyExists
	}
	if o.hasUniqueConflict(record, nil) {
		return idb.ErrAlreadyExists
	}
	o.records[id] = record
	return nil
}

func (o *stubObjectStore) Put(_ context.Context, record idb.Record) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	done := o.writeSchedule()
	defer done()
	id, _ := record["id"].(string)
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.hasUniqueConflict(record, &id) {
		return idb.ErrAlreadyExists
	}
	o.records[id] = record
	return nil
}

func (o *stubObjectStore) hasUniqueConflict(record idb.Record, ignoreID *string) bool {
	for _, idx := range o.schema.Indexes {
		if !idx.Unique {
			continue
		}
		for id, existing := range o.records {
			if ignoreID != nil && id == *ignoreID {
				continue
			}
			match := true
			for _, field := range idx.KeyPath {
				if existing[field] != record[field] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

func (o *stubObjectStore) Delete(_ context.Context, id string) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	done := o.writeSchedule()
	defer done()
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.records, id)
	return nil
}

func (o *stubObjectStore) Clear(_ context.Context) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	done := o.writeSchedule()
	defer done()
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records = make(map[string]idb.Record)
	return nil
}

func (o *stubObjectStore) GetAll(_ context.Context, r *idb.KeyRange) ([]idb.Record, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	c := o.newCursor(idb.CursorNext, false)
	c.applyKeyRange(r)
	out := make([]idb.Record, 0, len(c.keys))
	for _, key := range c.keys {
		out = append(out, c.snapshot[key])
	}
	return out, nil
}

func (o *stubObjectStore) GetAllKeys(_ context.Context, r *idb.KeyRange) ([]string, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	c := o.newCursor(idb.CursorNext, true)
	c.applyKeyRange(r)
	return append([]string(nil), c.keys...), nil
}

func (o *stubObjectStore) Count(_ context.Context, r *idb.KeyRange) (int64, error) {
	if o.db.Err != nil {
		return 0, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	c := o.newCursor(idb.CursorNext, true)
	c.applyKeyRange(r)
	return int64(len(c.keys)), nil
}

func (o *stubObjectStore) DeleteRange(_ context.Context, r idb.KeyRange) (int64, error) {
	if o.db.Err != nil {
		return 0, o.db.Err
	}
	done := o.writeSchedule()
	defer done()
	c := o.newCursor(idb.CursorNext, true)
	c.applyKeyRange(&r)
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, key := range c.keys {
		delete(o.records, key)
	}
	return int64(len(c.keys)), nil
}

func (o *stubObjectStore) Index(name string) idb.Index {
	return &stubIndex{store: o, name: name, schema: o.schema}
}

func (o *stubObjectStore) OpenCursor(_ context.Context, r *idb.KeyRange, dir idb.CursorDirection) (idb.Cursor, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	c := o.newCursor(dir, false)
	c.applyKeyRange(r)
	return c, nil
}

func (o *stubObjectStore) OpenKeyCursor(_ context.Context, r *idb.KeyRange, dir idb.CursorDirection) (idb.Cursor, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	c := o.newCursor(dir, true)
	c.applyKeyRange(r)
	return c, nil
}

func (o *stubObjectStore) newCursor(dir idb.CursorDirection, keysOnly bool) *stubCursor {
	o.mu.RLock()
	keys := make([]string, 0, len(o.records))
	snapshot := make(map[string]idb.Record, len(o.records))
	for k, r := range o.records {
		keys = append(keys, k)
		snapshot[k] = r
	}
	o.mu.RUnlock()

	sort.Strings(keys)
	if dir == idb.CursorPrev || dir == idb.CursorPrevUnique {
		sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	}

	reverse := dir == idb.CursorPrev || dir == idb.CursorPrevUnique
	unique := dir == idb.CursorNextUnique || dir == idb.CursorPrevUnique
	return &stubCursor{
		store:    o,
		keys:     keys,
		snapshot: snapshot,
		pos:      -1,
		keysOnly: keysOnly,
		reverse:  reverse,
		unique:   unique,
	}
}
