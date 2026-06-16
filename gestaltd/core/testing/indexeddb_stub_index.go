package coretesting

import (
	"context"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

type stubIndex struct {
	store  *stubObjectStore
	name   string
	schema idb.ObjectStoreOptions
}

func (idx *stubIndex) keyPath() []string {
	for _, is := range idx.schema.Indexes {
		if is.Name == idx.name {
			return is.KeyPath
		}
	}
	return nil
}

func (idx *stubIndex) Get(ctx context.Context, query any) (idb.Record, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	records, err := idx.GetAll(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, idb.ErrNotFound
	}
	return records[0], nil
}

func (idx *stubIndex) GetKey(ctx context.Context, query any) (string, error) {
	if idx.store.db.Err != nil {
		return "", idx.store.db.Err
	}
	rec, err := idx.Get(ctx, query)
	if err != nil {
		return "", err
	}
	id, _ := rec["id"].(string)
	return id, nil
}

func (idx *stubIndex) newCursor(dir idb.CursorDirection, query any, keysOnly bool) *stubCursor {
	c := idx.store.newCursor(dir, keysOnly)
	c.filterIndex = idx
	c.buildIndexKeys()
	c.applyQuery(query)
	return c
}

func (idx *stubIndex) GetAll(_ context.Context, query any, count ...uint32) ([]idb.Record, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	c := idx.newCursor(idb.CursorNext, query, false)
	out := make([]idb.Record, 0, len(c.keys))
	for _, key := range c.keys {
		out = append(out, c.snapshot[key])
	}
	if c.err != nil {
		return nil, c.err
	}
	return applyCountLimit(out, count...), nil
}

func (idx *stubIndex) GetAllKeys(ctx context.Context, query any, count ...uint32) ([]string, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	records, err := idx.GetAll(ctx, query, count...)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(records))
	for i, rec := range records {
		keys[i], _ = rec["id"].(string)
	}
	return keys, nil
}

func (idx *stubIndex) Count(ctx context.Context, query any) (int64, error) {
	if idx.store.db.Err != nil {
		return 0, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	c := idx.newCursor(idb.CursorNext, query, true)
	if c.err != nil {
		return 0, c.err
	}
	return int64(len(c.keys)), nil
}

func (idx *stubIndex) Delete(_ context.Context, query any) (int64, error) {
	if idx.store.db.Err != nil {
		return 0, idx.store.db.Err
	}
	done := idx.store.writeSchedule()
	defer done()
	c := idx.newCursor(idb.CursorNext, query, true)
	if c.err != nil {
		return 0, c.err
	}
	idx.store.mu.Lock()
	defer idx.store.mu.Unlock()
	for _, id := range c.keys {
		delete(idx.store.records, id)
	}
	return int64(len(c.keys)), nil
}

func (idx *stubIndex) OpenCursor(_ context.Context, query any, dir idb.CursorDirection) (idb.Cursor, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	c := idx.newCursor(dir, query, false)
	if c.err != nil {
		return nil, c.err
	}
	return c, nil
}

func (idx *stubIndex) OpenKeyCursor(_ context.Context, query any, dir idb.CursorDirection) (idb.Cursor, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	c := idx.newCursor(dir, query, true)
	if c.err != nil {
		return nil, c.err
	}
	return c, nil
}
