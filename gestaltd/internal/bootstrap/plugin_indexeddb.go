package bootstrap

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type pluginIndexedDBTransportOptions struct {
	StorePrefix   string
	AllowedStores []string
	DeniedStores  []string
}

func newPluginIndexedDBTransport(ds indexeddb.IndexedDB, opts pluginIndexedDBTransportOptions) indexeddb.IndexedDB {
	if ds == nil {
		return nil
	}
	if opts.StorePrefix == "" && len(opts.AllowedStores) == 0 && len(opts.DeniedStores) == 0 {
		return ds
	}
	allowed := make(map[string]struct{}, len(opts.AllowedStores))
	for _, store := range opts.AllowedStores {
		allowed[store] = struct{}{}
	}
	if len(allowed) == 0 {
		allowed = nil
	}
	denied := make(map[string]struct{}, len(opts.DeniedStores))
	for _, store := range opts.DeniedStores {
		denied[store] = struct{}{}
	}
	if len(denied) == 0 {
		denied = nil
	}
	return &pluginIndexedDBTransport{
		inner:   ds,
		prefix:  opts.StorePrefix,
		allowed: allowed,
		denied:  denied,
	}
}

type pluginIndexedDBTransport struct {
	inner   indexeddb.IndexedDB
	prefix  string
	allowed map[string]struct{}
	denied  map[string]struct{}
}

func (d *pluginIndexedDBTransport) translateStore(name string) (string, error) {
	if len(d.allowed) > 0 {
		if _, ok := d.allowed[name]; !ok {
			return "", indexeddb.ErrNotFound
		}
	}
	if len(d.denied) > 0 {
		if _, ok := d.denied[name]; ok {
			return "", indexeddb.ErrNotFound
		}
	}
	return d.prefix + name, nil
}

func (d *pluginIndexedDBTransport) ObjectStore(name string) indexeddb.ObjectStore {
	storeName, err := d.translateStore(name)
	if err != nil {
		return missingObjectStore{}
	}
	return d.inner.ObjectStore(storeName)
}

func (d *pluginIndexedDBTransport) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	storeName, err := d.translateStore(name)
	if err != nil {
		return err
	}
	return d.inner.CreateObjectStore(ctx, storeName, schema)
}

func (d *pluginIndexedDBTransport) DeleteObjectStore(ctx context.Context, name string) error {
	storeName, err := d.translateStore(name)
	if err != nil {
		return err
	}
	return d.inner.DeleteObjectStore(ctx, storeName)
}

func (d *pluginIndexedDBTransport) Ping(ctx context.Context) error {
	return d.inner.Ping(ctx)
}

func (d *pluginIndexedDBTransport) Close() error {
	return d.inner.Close()
}

type missingObjectStore struct{}

func (missingObjectStore) Get(context.Context, string) (indexeddb.Record, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingObjectStore) GetKey(context.Context, string) (string, error) {
	return "", indexeddb.ErrNotFound
}

func (missingObjectStore) Add(context.Context, indexeddb.Record) error {
	return indexeddb.ErrNotFound
}

func (missingObjectStore) Put(context.Context, indexeddb.Record) error {
	return indexeddb.ErrNotFound
}

func (missingObjectStore) Delete(context.Context, string) error {
	return indexeddb.ErrNotFound
}

func (missingObjectStore) Clear(context.Context) error {
	return indexeddb.ErrNotFound
}

func (missingObjectStore) GetAll(context.Context, *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingObjectStore) GetAllKeys(context.Context, *indexeddb.KeyRange) ([]string, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingObjectStore) Count(context.Context, *indexeddb.KeyRange) (int64, error) {
	return 0, indexeddb.ErrNotFound
}

func (missingObjectStore) DeleteRange(context.Context, indexeddb.KeyRange) (int64, error) {
	return 0, indexeddb.ErrNotFound
}

func (missingObjectStore) Index(string) indexeddb.Index {
	return missingIndex{}
}

func (missingObjectStore) OpenCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingObjectStore) OpenKeyCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrNotFound
}

type missingIndex struct{}

func (missingIndex) Get(context.Context, ...any) (indexeddb.Record, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingIndex) GetKey(context.Context, ...any) (string, error) {
	return "", indexeddb.ErrNotFound
}

func (missingIndex) GetAll(context.Context, *indexeddb.KeyRange, ...any) ([]indexeddb.Record, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingIndex) GetAllKeys(context.Context, *indexeddb.KeyRange, ...any) ([]string, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingIndex) Count(context.Context, *indexeddb.KeyRange, ...any) (int64, error) {
	return 0, indexeddb.ErrNotFound
}

func (missingIndex) Delete(context.Context, ...any) (int64, error) {
	return 0, indexeddb.ErrNotFound
}

func (missingIndex) OpenCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection, ...any) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingIndex) OpenKeyCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection, ...any) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrNotFound
}
