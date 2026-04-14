package bootstrap

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type pluginIndexedDBTransportOptions struct {
	StorePrefix       string
	LegacyStorePrefix string
	AllowedStores     []string
	DeniedStores      []string
}

func newPluginIndexedDBTransport(ds indexeddb.IndexedDB, opts pluginIndexedDBTransportOptions) indexeddb.IndexedDB {
	if ds == nil {
		return nil
	}
	if opts.StorePrefix == "" && opts.LegacyStorePrefix == "" && len(opts.AllowedStores) == 0 && len(opts.DeniedStores) == 0 {
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
		inner:        ds,
		prefix:       opts.StorePrefix,
		legacyPrefix: opts.LegacyStorePrefix,
		allowed:      allowed,
		denied:       denied,
	}
}

type pluginIndexedDBTransport struct {
	inner        indexeddb.IndexedDB
	prefix       string
	legacyPrefix string
	allowed      map[string]struct{}
	denied       map[string]struct{}
}

func (d *pluginIndexedDBTransport) translateStore(name string) (string, string, error) {
	if len(d.allowed) > 0 {
		if _, ok := d.allowed[name]; !ok {
			return "", "", indexeddb.ErrNotFound
		}
	}
	if len(d.denied) > 0 {
		if _, ok := d.denied[name]; ok {
			return "", "", indexeddb.ErrNotFound
		}
	}
	return d.prefix + name, d.legacyPrefix + name, nil
}

func (d *pluginIndexedDBTransport) ObjectStore(name string) indexeddb.ObjectStore {
	primary, legacy, err := d.translateStore(name)
	if err != nil {
		return missingObjectStore{}
	}
	return &pluginIndexedDBObjectStore{
		inner:   d.inner,
		primary: primary,
		legacy:  legacy,
	}
}

func (d *pluginIndexedDBTransport) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	primary, legacy, err := d.translateStore(name)
	if err != nil {
		return err
	}
	storeName, exists, err := resolveActiveStoreName(ctx, d.inner, primary, legacy)
	if err != nil {
		return err
	}
	if !exists {
		storeName = primary
	}
	return d.inner.CreateObjectStore(ctx, storeName, schema)
}

func (d *pluginIndexedDBTransport) DeleteObjectStore(ctx context.Context, name string) error {
	primary, legacy, err := d.translateStore(name)
	if err != nil {
		return err
	}
	storeName, exists, err := resolveActiveStoreName(ctx, d.inner, primary, legacy)
	if err != nil {
		return err
	}
	if !exists {
		storeName = primary
	}
	return d.inner.DeleteObjectStore(ctx, storeName)
}

func (d *pluginIndexedDBTransport) Ping(ctx context.Context) error {
	return d.inner.Ping(ctx)
}

func (d *pluginIndexedDBTransport) Close() error {
	return d.inner.Close()
}

type pluginIndexedDBObjectStore struct {
	inner   indexeddb.IndexedDB
	primary string
	legacy  string
}

func (s *pluginIndexedDBObjectStore) resolve(ctx context.Context) (indexeddb.ObjectStore, error) {
	storeName, _, err := resolveActiveStoreName(ctx, s.inner, s.primary, s.legacy)
	if err != nil {
		return nil, err
	}
	return s.inner.ObjectStore(storeName), nil
}

func (s *pluginIndexedDBObjectStore) Get(ctx context.Context, id string) (indexeddb.Record, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.Get(ctx, id)
}

func (s *pluginIndexedDBObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return "", err
	}
	return store.GetKey(ctx, id)
}

func (s *pluginIndexedDBObjectStore) Add(ctx context.Context, record indexeddb.Record) error {
	store, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return store.Add(ctx, record)
}

func (s *pluginIndexedDBObjectStore) Put(ctx context.Context, record indexeddb.Record) error {
	store, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return store.Put(ctx, record)
}

func (s *pluginIndexedDBObjectStore) Delete(ctx context.Context, id string) error {
	store, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return store.Delete(ctx, id)
}

func (s *pluginIndexedDBObjectStore) Clear(ctx context.Context) error {
	store, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return store.Clear(ctx)
}

func (s *pluginIndexedDBObjectStore) GetAll(ctx context.Context, r *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.GetAll(ctx, r)
}

func (s *pluginIndexedDBObjectStore) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange) ([]string, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.GetAllKeys(ctx, r)
}

func (s *pluginIndexedDBObjectStore) Count(ctx context.Context, r *indexeddb.KeyRange) (int64, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return 0, err
	}
	return store.Count(ctx, r)
}

func (s *pluginIndexedDBObjectStore) DeleteRange(ctx context.Context, r indexeddb.KeyRange) (int64, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return 0, err
	}
	return store.DeleteRange(ctx, r)
}

func (s *pluginIndexedDBObjectStore) Index(name string) indexeddb.Index {
	return &pluginIndexedDBIndex{
		store: s,
		name:  name,
	}
}

func (s *pluginIndexedDBObjectStore) OpenCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.OpenCursor(ctx, r, dir)
}

func (s *pluginIndexedDBObjectStore) OpenKeyCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.OpenKeyCursor(ctx, r, dir)
}

type pluginIndexedDBIndex struct {
	store *pluginIndexedDBObjectStore
	name  string
}

func (i *pluginIndexedDBIndex) resolve(ctx context.Context) (indexeddb.Index, error) {
	store, err := i.store.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.Index(i.name), nil
}

func (i *pluginIndexedDBIndex) Get(ctx context.Context, values ...any) (indexeddb.Record, error) {
	index, err := i.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return index.Get(ctx, values...)
}

func (i *pluginIndexedDBIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	index, err := i.resolve(ctx)
	if err != nil {
		return "", err
	}
	return index.GetKey(ctx, values...)
}

func (i *pluginIndexedDBIndex) GetAll(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]indexeddb.Record, error) {
	index, err := i.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return index.GetAll(ctx, r, values...)
}

func (i *pluginIndexedDBIndex) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]string, error) {
	index, err := i.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return index.GetAllKeys(ctx, r, values...)
}

func (i *pluginIndexedDBIndex) Count(ctx context.Context, r *indexeddb.KeyRange, values ...any) (int64, error) {
	index, err := i.resolve(ctx)
	if err != nil {
		return 0, err
	}
	return index.Count(ctx, r, values...)
}

func (i *pluginIndexedDBIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	index, err := i.resolve(ctx)
	if err != nil {
		return 0, err
	}
	return index.Delete(ctx, values...)
}

func (i *pluginIndexedDBIndex) OpenCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, values ...any) (indexeddb.Cursor, error) {
	index, err := i.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return index.OpenCursor(ctx, r, dir, values...)
}

func (i *pluginIndexedDBIndex) OpenKeyCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, values ...any) (indexeddb.Cursor, error) {
	index, err := i.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return index.OpenKeyCursor(ctx, r, dir, values...)
}

func resolveActiveStoreName(ctx context.Context, db indexeddb.IndexedDB, primary, legacy string) (string, bool, error) {
	if exists, err := storeExists(ctx, db, primary); err != nil {
		return "", false, err
	} else if exists {
		return primary, true, nil
	}
	if legacy != "" && legacy != primary {
		if exists, err := storeExists(ctx, db, legacy); err != nil {
			return "", false, err
		} else if exists {
			return legacy, true, nil
		}
	}
	return primary, false, nil
}

func storeExists(ctx context.Context, db indexeddb.IndexedDB, storeName string) (bool, error) {
	if storeName == "" {
		return false, nil
	}
	type objectStoreExistenceChecker interface {
		HasObjectStore(name string) bool
	}
	if checker, ok := db.(objectStoreExistenceChecker); ok {
		return checker.HasObjectStore(storeName), nil
	}
	_, err := db.ObjectStore(storeName).Count(ctx, nil)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, indexeddb.ErrNotFound):
		return false, nil
	default:
		return false, err
	}
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
