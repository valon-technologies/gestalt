package bootstrap

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type pluginIndexedDBTransportOptions struct {
	StorePrefix string
}

func newPluginIndexedDBTransport(ds indexeddb.IndexedDB, opts pluginIndexedDBTransportOptions) indexeddb.IndexedDB {
	if ds == nil {
		return nil
	}
	if opts.StorePrefix == "" {
		return ds
	}
	return &pluginIndexedDBTransport{
		inner:  ds,
		prefix: opts.StorePrefix,
	}
}

type pluginIndexedDBTransport struct {
	inner  indexeddb.IndexedDB
	prefix string
}

func (d *pluginIndexedDBTransport) translateStore(name string) string {
	return d.prefix + name
}

func (d *pluginIndexedDBTransport) ObjectStore(name string) indexeddb.ObjectStore {
	return d.inner.ObjectStore(d.translateStore(name))
}

func (d *pluginIndexedDBTransport) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	return d.inner.CreateObjectStore(ctx, d.translateStore(name), schema)
}

func (d *pluginIndexedDBTransport) DeleteObjectStore(ctx context.Context, name string) error {
	return d.inner.DeleteObjectStore(ctx, d.translateStore(name))
}

func (d *pluginIndexedDBTransport) Ping(ctx context.Context) error {
	return d.inner.Ping(ctx)
}

func (d *pluginIndexedDBTransport) Close() error {
	return d.inner.Close()
}
