package host

import (
	"context"
	"fmt"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
)

type HostClient struct {
	client    proto.IndexedDBClient
	rpcConfig rpcConfig
}

// Close is a no-op because this client uses shared transport.
func (db *HostClient) Close() error { return nil }

var (
	_ idb.Database    = (*HostClient)(nil)
	_ idb.ObjectStore = (*ObjectStoreClient)(nil)
	_ idb.Index       = (*IndexClient)(nil)
	_ idb.Transaction = (*hostTransaction)(nil)
	_ idb.Cursor      = (*hostCursor)(nil)
)

// CreateObjectStore creates a named object store with the supplied schema.
func (db *HostClient) CreateObjectStore(ctx context.Context, name string, schema idb.ObjectStoreOptions) (idb.ObjectStore, error) {
	ctx, cancel := db.rpcConfig.withDeadline(ctx)
	defer cancel()
	indexes := make([]*proto.IndexSchema, len(schema.Indexes))
	for i, idx := range schema.Indexes {
		indexes[i] = &proto.IndexSchema{Name: idx.Name, KeyPath: idx.KeyPath, Unique: idx.Unique}
	}
	columns := make([]*proto.ColumnDef, len(schema.Columns))
	for i, col := range schema.Columns {
		columns[i] = &proto.ColumnDef{
			Name:       col.Name,
			Type:       int32(col.Type),
			PrimaryKey: col.PrimaryKey,
			NotNull:    col.NotNull,
			Unique:     col.Unique,
		}
	}
	_, err := db.client.CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{
		Name: name, Schema: &proto.ObjectStoreSchema{Indexes: indexes, Columns: columns},
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	return db.ObjectStore(name), nil
}

// DeleteObjectStore removes a named object store.
func (db *HostClient) DeleteObjectStore(ctx context.Context, name string) error {
	ctx, cancel := db.rpcConfig.withDeadline(ctx)
	defer cancel()
	_, err := db.client.DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{Name: name})
	return grpcErr(err)
}

// ObjectStore returns a typed handle for working with one object store.
func (db *HostClient) ObjectStore(name string) idb.ObjectStore {
	return &ObjectStoreClient{client: db.client, store: name, rpcConfig: db.rpcConfig}
}

// Transaction starts an explicit IndexedDB transaction over the supplied object
// store scope.
func (db *HostClient) Transaction(ctx context.Context, stores []string, mode idb.TransactionMode, opts idb.TransactionOptions) (idb.Transaction, error) {
	if mode == idb.TransactionVersionChange {
		return nil, fmt.Errorf("%w: versionchange transactions", idb.ErrUnsupported)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := db.client.Transaction(streamCtx)
	if err != nil {
		cancel()
		return nil, grpcErr(err)
	}
	if err := stream.Send(&proto.TransactionClientMessage{
		Msg: &proto.TransactionClientMessage_Begin{Begin: &proto.BeginTransactionRequest{
			Stores:         stores,
			Mode:           transactionModeToProto(mode),
			DurabilityHint: durabilityHintToProto(opts.DurabilityHint),
		}},
	}); err != nil {
		_ = stream.CloseSend()
		cancel()
		return nil, grpcErr(err)
	}
	resp, err := stream.Recv()
	if err != nil {
		_ = stream.CloseSend()
		cancel()
		return nil, grpcErr(err)
	}
	if resp.GetBegin() == nil {
		_ = stream.CloseSend()
		cancel()
		return nil, fmt.Errorf("indexeddb: expected transaction begin response")
	}
	return &hostTransaction{stream: stream, cancel: cancel}, nil
}

// ObjectStoreClient provides CRUD, range-query, and cursor access to one
// object store.
type ObjectStoreClient struct {
	client proto.IndexedDBClient
	store  string
	rpcConfig
}

// Get loads one record by primary key.
func (o *ObjectStoreClient) Get(ctx context.Context, id string) (idb.Record, error) {
	ctx, cancel := o.withDeadline(ctx)
	defer cancel()
	resp, err := o.client.Get(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return nil, grpcErr(err)
	}
	record, err := recordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

// GetKey resolves the primary key for the supplied lookup id.
func (o *ObjectStoreClient) GetKey(ctx context.Context, id string) (string, error) {
	ctx, cancel := o.withDeadline(ctx)
	defer cancel()
	resp, err := o.client.GetKey(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return "", grpcErr(err)
	}
	return resp.GetKey(), nil
}

// Add inserts a new record and fails if its primary key already exists.
func (o *ObjectStoreClient) Add(ctx context.Context, record idb.Record) error {
	ctx, cancel := o.withDeadline(ctx)
	defer cancel()
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Add(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcErr(err)
}

// Put upserts a record by primary key.
func (o *ObjectStoreClient) Put(ctx context.Context, record idb.Record) error {
	ctx, cancel := o.withDeadline(ctx)
	defer cancel()
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Put(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcErr(err)
}

// Delete removes one record by primary key.
func (o *ObjectStoreClient) Delete(ctx context.Context, id string) error {
	ctx, cancel := o.withDeadline(ctx)
	defer cancel()
	_, err := o.client.Delete(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	return grpcErr(err)
}

// Clear removes every record from the object store.
func (o *ObjectStoreClient) Clear(ctx context.Context) error {
	ctx, cancel := o.withDeadline(ctx)
	defer cancel()
	_, err := o.client.Clear(ctx, &proto.ObjectStoreNameRequest{Store: o.store})
	return grpcErr(err)
}

// GetAll loads all records that match r.
func (o *ObjectStoreClient) GetAll(ctx context.Context, r *idb.KeyRange) ([]idb.Record, error) {
	ctx, cancel := o.withDeadline(ctx)
	defer cancel()
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAll(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcErr(err)
	}
	records, err := recordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

// GetAllKeys loads the primary keys for all records that match r.
func (o *ObjectStoreClient) GetAllKeys(ctx context.Context, r *idb.KeyRange) ([]string, error) {
	ctx, cancel := o.withDeadline(ctx)
	defer cancel()
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAllKeys(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcErr(err)
	}
	return resp.GetKeys(), nil
}

// Count returns the number of records that match r.
func (o *ObjectStoreClient) Count(ctx context.Context, r *idb.KeyRange) (int64, error) {
	ctx, cancel := o.withDeadline(ctx)
	defer cancel()
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.Count(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetCount(), nil
}

// DeleteRange removes all records that match r and reports how many were
// deleted.
func (o *ObjectStoreClient) DeleteRange(ctx context.Context, r idb.KeyRange) (int64, error) {
	ctx, cancel := o.withDeadline(ctx)
	defer cancel()
	kr, err := krToProto(&r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.DeleteRange(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetDeleted(), nil
}

// OpenCursor opens a full-value cursor over the object store.
func (o *ObjectStoreClient) OpenCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection) (idb.Cursor, error) {
	return openCursor(ctx, o.client, o.store, "", r, dir, false, nil)
}

// OpenKeyCursor opens a key-only cursor over the object store.
func (o *ObjectStoreClient) OpenKeyCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection) (idb.Cursor, error) {
	return openCursor(ctx, o.client, o.store, "", r, dir, true, nil)
}

// Index returns a typed handle for a secondary index on the object store.
func (o *ObjectStoreClient) Index(name string) idb.Index {
	return &IndexClient{client: o.client, store: o.store, index: name, rpcConfig: o.rpcConfig}
}

// IndexClient provides lookup and cursor access through one secondary index.
type IndexClient struct {
	client proto.IndexedDBClient
	store  string
	index  string
	rpcConfig
}

// Get loads the first record that matches the supplied index key.
func (idx *IndexClient) Get(ctx context.Context, values ...any) (idb.Record, error) {
	ctx, cancel := idx.withDeadline(ctx)
	defer cancel()
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGet(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	record, err := recordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

// GetKey resolves the primary key for the first row that matches values.
func (idx *IndexClient) GetKey(ctx context.Context, values ...any) (string, error) {
	ctx, cancel := idx.withDeadline(ctx)
	defer cancel()
	vals, err := anyToProtoValues(values)
	if err != nil {
		return "", err
	}
	resp, err := idx.client.IndexGetKey(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals,
	})
	if err != nil {
		return "", grpcErr(err)
	}
	return resp.GetKey(), nil
}

// GetAll loads every record that matches values and r.
func (idx *IndexClient) GetAll(ctx context.Context, r *idb.KeyRange, values ...any) ([]idb.Record, error) {
	ctx, cancel := idx.withDeadline(ctx)
	defer cancel()
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAll(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	records, err := recordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

// GetAllKeys loads every primary key that matches values and r.
func (idx *IndexClient) GetAllKeys(ctx context.Context, r *idb.KeyRange, values ...any) ([]string, error) {
	ctx, cancel := idx.withDeadline(ctx)
	defer cancel()
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAllKeys(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	return resp.GetKeys(), nil
}

// Count returns the number of rows that match values and r.
func (idx *IndexClient) Count(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
	ctx, cancel := idx.withDeadline(ctx)
	defer cancel()
	vals, err := anyToProtoValues(values)
	if err != nil {
		return 0, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexCount(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetCount(), nil
}

// Delete removes all rows that match values.
func (idx *IndexClient) Delete(ctx context.Context, values ...any) (int64, error) {
	return idx.DeleteRange(ctx, nil, values...)
}

// DeleteRange removes all rows that match values and r.
func (idx *IndexClient) DeleteRange(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
	ctx, cancel := idx.withDeadline(ctx)
	defer cancel()
	vals, err := anyToProtoValues(values)
	if err != nil {
		return 0, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexDelete(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetDeleted(), nil
}

// OpenCursor opens a full-value cursor over one secondary index.
func (idx *IndexClient) OpenCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection, values ...any) (idb.Cursor, error) {
	return openCursor(ctx, idx.client, idx.store, idx.index, r, dir, false, values)
}

// OpenKeyCursor opens a key-only cursor over one secondary index.
func (idx *IndexClient) OpenKeyCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection, values ...any) (idb.Cursor, error) {
	return openCursor(ctx, idx.client, idx.store, idx.index, r, dir, true, values)
}

// HostTransaction is the host-service transaction implementation.
type HostTransaction = hostTransaction
