package coretesting

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type stubIndex struct {
	store  *stubObjectStore
	name   string
	schema indexeddb.ObjectStoreSchema
}

func (idx *stubIndex) keyPath() []string {
	for _, is := range idx.schema.Indexes {
		if is.Name == idx.name {
			return is.KeyPath
		}
	}
	return nil
}

func (idx *stubIndex) matches(record indexeddb.Record, values []any) bool {
	kp := idx.keyPath()
	if kp == nil {
		return false
	}
	for i, field := range kp {
		if i >= len(values) {
			break
		}
		rv := record[field]
		if rv != values[i] {
			return false
		}
	}
	return true
}

func (idx *stubIndex) Get(ctx context.Context, values ...any) (indexeddb.Record, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	records, err := idx.GetAll(ctx, nil, values...)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, indexeddb.ErrNotFound
	}
	return records[0], nil
}

func (idx *stubIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	if idx.store.db.Err != nil {
		return "", idx.store.db.Err
	}
	rec, err := idx.Get(ctx, values...)
	if err != nil {
		return "", err
	}
	id, _ := rec["id"].(string)
	return id, nil
}

func (idx *stubIndex) newCursor(dir indexeddb.CursorDirection, r *indexeddb.KeyRange, keysOnly bool, values ...any) *stubCursor {
	c := idx.store.newCursor(dir, keysOnly)
	c.filterIndex = idx
	c.filterValues = values
	c.applyIndexFilter()
	c.buildIndexKeys()
	c.applyKeyRange(r)
	return c
}

func (idx *stubIndex) GetAll(_ context.Context, r *indexeddb.KeyRange, values ...any) ([]indexeddb.Record, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	c := idx.newCursor(indexeddb.CursorNext, r, false, values...)
	out := make([]indexeddb.Record, 0, len(c.keys))
	for _, key := range c.keys {
		out = append(out, c.snapshot[key])
	}
	return out, nil
}

func (idx *stubIndex) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]string, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	records, err := idx.GetAll(ctx, r, values...)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(records))
	for i, rec := range records {
		keys[i], _ = rec["id"].(string)
	}
	return keys, nil
}

func (idx *stubIndex) Count(ctx context.Context, r *indexeddb.KeyRange, values ...any) (int64, error) {
	if idx.store.db.Err != nil {
		return 0, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	c := idx.newCursor(indexeddb.CursorNext, r, true, values...)
	return int64(len(c.keys)), nil
}

func (idx *stubIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	return idx.DeleteRange(ctx, nil, values...)
}

func (idx *stubIndex) DeleteRange(_ context.Context, r *indexeddb.KeyRange, values ...any) (int64, error) {
	if idx.store.db.Err != nil {
		return 0, idx.store.db.Err
	}
	done := idx.store.writeSchedule()
	defer done()
	c := idx.newCursor(indexeddb.CursorNext, r, true, values...)
	idx.store.mu.Lock()
	defer idx.store.mu.Unlock()
	for _, id := range c.keys {
		delete(idx.store.records, id)
	}
	return int64(len(c.keys)), nil
}

func (idx *stubIndex) OpenCursor(_ context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, values ...any) (indexeddb.Cursor, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	return idx.newCursor(dir, r, false, values...), nil
}

func (idx *stubIndex) OpenKeyCursor(_ context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, values ...any) (indexeddb.Cursor, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	return idx.newCursor(dir, r, true, values...), nil
}
