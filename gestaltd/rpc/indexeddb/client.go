package indexeddb

import (
	"context"
	"fmt"
	"sync"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// clientDB is the gRPC implementation of idb.Database.
type clientDB struct {
	client proto.IndexedDBClient
	opts   Options
}

// Close is a no-op because this client uses shared transport.
func (db *clientDB) Close() error { return nil }

var (
	_ idb.Database     = (*clientDB)(nil)
	_ idb.ObjectStore  = (*objectStore)(nil)
	_ idb.RangeDeleter = (*objectStore)(nil)
	_ idb.Index        = (*indexClient)(nil)
	_ idb.MutableIndex = (*indexClient)(nil)
	_ idb.Transaction  = (*hostTx)(nil)
	_ idb.Cursor       = (*rpcCursor)(nil)
)

// CreateObjectStore creates a named object store with the supplied schema.
func (db *clientDB) CreateObjectStore(ctx context.Context, name string, schema idb.ObjectStoreOptions) (idb.ObjectStore, error) {
	ctx, cancel := db.callCtx(ctx)
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
func (db *clientDB) DeleteObjectStore(ctx context.Context, name string) error {
	ctx, cancel := db.callCtx(ctx)
	defer cancel()
	_, err := db.client.DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{Name: name})
	return grpcErr(err)
}

// idb.ObjectStore returns a typed handle for working with one object store.
func (db *clientDB) ObjectStore(name string) idb.ObjectStore {
	return &objectStore{client: db.client, store: name, opts: db.opts}
}

// idb.Transaction starts an explicit IndexedDB transaction over the supplied object
// store scope.
func (db *clientDB) Transaction(ctx context.Context, stores []string, mode idb.TransactionMode, opts idb.TransactionOptions) (idb.Transaction, error) {
	ctx, cancel := db.callCtx(ctx)
	defer cancel()
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
	return &hostTx{stream: stream, cancel: cancel}, nil
}

// objectStore provides CRUD, range-query, and cursor access to one
// object store.
type objectStore struct {
	client proto.IndexedDBClient
	store  string
	opts   Options
}

// Get loads one record by primary key.
func (o *objectStore) Get(ctx context.Context, id string) (idb.Record, error) {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
	defer cancel()
	resp, err := o.client.Get(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return nil, grpcErr(err)
	}
	record, err := idb.RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

// GetKey resolves the primary key for the supplied lookup id.
func (o *objectStore) GetKey(ctx context.Context, id string) (string, error) {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
	defer cancel()
	resp, err := o.client.GetKey(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return "", grpcErr(err)
	}
	return resp.GetKey(), nil
}

// Add inserts a new record and fails if its primary key already exists.
func (o *objectStore) Add(ctx context.Context, record idb.Record) error {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
	defer cancel()
	pbRecord, err := idb.RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Add(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcErr(err)
}

// Put upserts a record by primary key.
func (o *objectStore) Put(ctx context.Context, record idb.Record) error {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
	defer cancel()
	pbRecord, err := idb.RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Put(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcErr(err)
}

// Delete removes one record by primary key.
func (o *objectStore) Delete(ctx context.Context, id string) error {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
	defer cancel()
	_, err := o.client.Delete(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	return grpcErr(err)
}

// Clear removes every record from the object store.
func (o *objectStore) Clear(ctx context.Context) error {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
	defer cancel()
	_, err := o.client.Clear(ctx, &proto.ObjectStoreNameRequest{Store: o.store})
	return grpcErr(err)
}

// GetAll loads all records that match r.
func (o *objectStore) GetAll(ctx context.Context, r *idb.KeyRange) ([]idb.Record, error) {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
	defer cancel()
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAll(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcErr(err)
	}
	records, err := idb.RecordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

// GetAllKeys loads the primary keys for all records that match r.
func (o *objectStore) GetAllKeys(ctx context.Context, r *idb.KeyRange) ([]string, error) {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
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
func (o *objectStore) Count(ctx context.Context, r *idb.KeyRange) (int64, error) {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
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
func (o *objectStore) DeleteRange(ctx context.Context, r idb.KeyRange) (int64, error) {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
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
func (o *objectStore) OpenCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection) (idb.Cursor, error) {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
	defer cancel()
	return openCursor(ctx, o.client, o.store, "", r, dir, false, nil)
}

// OpenKeyCursor opens a key-only cursor over the object store.
func (o *objectStore) OpenKeyCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection) (idb.Cursor, error) {
	ctx, cancel := attachTimeout(ctx, o.opts.UnaryTimeout)
	defer cancel()
	return openCursor(ctx, o.client, o.store, "", r, dir, true, nil)
}

// idb.Index returns a typed handle for a secondary index on the object store.
func (o *objectStore) Index(name string) idb.Index {
	return &indexClient{client: o.client, store: o.store, index: name, opts: o.opts}
}

// indexClient provides lookup and cursor access through one secondary index.
type indexClient struct {
	client proto.IndexedDBClient
	store  string
	index  string
	opts   Options
}

// Get loads the first record that matches the supplied index key.
func (idx *indexClient) Get(ctx context.Context, values ...any) (idb.Record, error) {
	ctx, cancel := attachTimeout(ctx, idx.opts.UnaryTimeout)
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
	record, err := idb.RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

// GetKey resolves the primary key for the first row that matches values.
func (idx *indexClient) GetKey(ctx context.Context, values ...any) (string, error) {
	ctx, cancel := attachTimeout(ctx, idx.opts.UnaryTimeout)
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
func (idx *indexClient) GetAll(ctx context.Context, r *idb.KeyRange, values ...any) ([]idb.Record, error) {
	ctx, cancel := attachTimeout(ctx, idx.opts.UnaryTimeout)
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
	records, err := idb.RecordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

// GetAllKeys loads every primary key that matches values and r.
func (idx *indexClient) GetAllKeys(ctx context.Context, r *idb.KeyRange, values ...any) ([]string, error) {
	ctx, cancel := attachTimeout(ctx, idx.opts.UnaryTimeout)
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
func (idx *indexClient) Count(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
	ctx, cancel := attachTimeout(ctx, idx.opts.UnaryTimeout)
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
func (idx *indexClient) Delete(ctx context.Context, values ...any) (int64, error) {
	ctx, cancel := attachTimeout(ctx, idx.opts.UnaryTimeout)
	defer cancel()
	return idx.DeleteRange(ctx, nil, values...)
}

// DeleteRange removes all rows that match values and r.
func (idx *indexClient) DeleteRange(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
	ctx, cancel := attachTimeout(ctx, idx.opts.UnaryTimeout)
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
func (idx *indexClient) OpenCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection, values ...any) (idb.Cursor, error) {
	ctx, cancel := attachTimeout(ctx, idx.opts.UnaryTimeout)
	defer cancel()
	return openCursor(ctx, idx.client, idx.store, idx.index, r, dir, false, values)
}

// OpenKeyCursor opens a key-only cursor over one secondary index.
func (idx *indexClient) OpenKeyCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection, values ...any) (idb.Cursor, error) {
	ctx, cancel := attachTimeout(ctx, idx.opts.UnaryTimeout)
	defer cancel()
	return openCursor(ctx, idx.client, idx.store, idx.index, r, dir, true, values)
}

// hostTx is an explicit IndexedDB transaction over a fixed store scope.
type hostTx struct {
	stream proto.IndexedDB_TransactionClient
	cancel context.CancelFunc
	mu     sync.Mutex
	nextID uint64
	done   bool
	err    error
}

// idb.ObjectStore returns a transaction-scoped object store handle.
func (tx *hostTx) ObjectStore(name string) idb.TransactionObjectStore {
	return &txObjectStore{tx: tx, store: name}
}

// Commit atomically commits all writes made in the transaction.
func (tx *hostTx) Commit(ctx context.Context) error {
	_ = ctx
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return err
		}
		return idb.ErrTransactionDone
	}
	if tx.err != nil {
		err := tx.err
		tx.mu.Unlock()
		return err
	}
	tx.done = true

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Commit{Commit: &proto.TransactionCommitRequest{}}}); err != nil {
		return tx.failLocked(grpcErr(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return tx.failLocked(grpcErr(err))
	}
	commit := resp.GetCommit()
	if commit == nil {
		return tx.failLocked(fmt.Errorf("indexeddb: expected transaction commit response"))
	}
	if err := rpcStatusErr(commit.GetError()); err != nil {
		return tx.failLocked(err)
	}
	tx.mu.Unlock()
	tx.cleanup()
	return nil
}

// Abort rolls back the transaction.
func (tx *hostTx) Abort(ctx context.Context) error {
	_ = ctx
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return err
		}
		return idb.ErrTransactionDone
	}
	tx.done = true

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Abort{Abort: &proto.TransactionAbortRequest{}}}); err != nil {
		return tx.failLocked(grpcErr(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return tx.failLocked(grpcErr(err))
	}
	abort := resp.GetAbort()
	if abort == nil {
		return tx.failLocked(fmt.Errorf("indexeddb: expected transaction abort response"))
	}
	if err := rpcStatusErr(abort.GetError()); err != nil {
		return tx.failLocked(err)
	}
	tx.mu.Unlock()
	tx.cleanup()
	return nil
}

func (tx *hostTx) sendOperation(op *proto.TransactionOperation) (*proto.TransactionOperationResponse, error) {
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, idb.ErrTransactionDone
	}
	if tx.err != nil {
		err := tx.err
		tx.mu.Unlock()
		return nil, err
	}
	tx.nextID++
	op.RequestId = tx.nextID

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Operation{Operation: op}}); err != nil {
		return nil, tx.failLocked(grpcErr(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return nil, tx.failLocked(grpcErr(err))
	}
	opResp := resp.GetOperation()
	if opResp == nil {
		return nil, tx.failLocked(fmt.Errorf("indexeddb: expected transaction operation response"))
	}
	if opResp.GetRequestId() != op.GetRequestId() {
		return nil, tx.failLocked(fmt.Errorf("indexeddb: response request id %d does not match %d", opResp.GetRequestId(), op.GetRequestId()))
	}
	if err := rpcStatusErr(opResp.GetError()); err != nil {
		tx.done = true
		tx.err = err
		tx.mu.Unlock()
		tx.cleanup()
		return nil, err
	}
	tx.mu.Unlock()
	return opResp, nil
}

func (tx *hostTx) failLocked(err error) error {
	tx.err = err
	tx.done = true
	tx.mu.Unlock()
	tx.cleanup()
	return err
}

func (tx *hostTx) cleanup() {
	if tx.stream != nil {
		_ = tx.stream.CloseSend()
		tx.stream = nil
	}
	if tx.cancel != nil {
		tx.cancel()
		tx.cancel = nil
	}
}

// idb.TransactionObjectStore provides transaction-scoped object-store operations.
type txObjectStore struct {
	tx    *hostTx
	store string
}

func (s *txObjectStore) Get(ctx context.Context, id string) (idb.Record, error) {
	_ = ctx
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Get{Get: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	if err != nil {
		return nil, err
	}
	record, err := idb.RecordFromProto(resp.GetRecord().GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (s *txObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	_ = ctx
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetKey{GetKey: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	if err != nil {
		return "", err
	}
	return resp.GetKey().GetKey(), nil
}

func (s *txObjectStore) Add(ctx context.Context, record idb.Record) error {
	_ = ctx
	pbRecord, err := idb.RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Add{Add: &proto.RecordRequest{Store: s.store, Record: pbRecord}}})
	return err
}

func (s *txObjectStore) Put(ctx context.Context, record idb.Record) error {
	_ = ctx
	pbRecord, err := idb.RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Put{Put: &proto.RecordRequest{Store: s.store, Record: pbRecord}}})
	return err
}

func (s *txObjectStore) Delete(ctx context.Context, id string) error {
	_ = ctx
	_, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Delete{Delete: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	return err
}

func (s *txObjectStore) Clear(ctx context.Context) error {
	_ = ctx
	_, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Clear{Clear: &proto.ObjectStoreNameRequest{Store: s.store}}})
	return err
}

func (s *txObjectStore) GetAll(ctx context.Context, r *idb.KeyRange) ([]idb.Record, error) {
	_ = ctx
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetAll{GetAll: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return nil, err
	}
	records, err := idb.RecordsFromProto(resp.GetRecords().GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (s *txObjectStore) GetAllKeys(ctx context.Context, r *idb.KeyRange) ([]string, error) {
	_ = ctx
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetAllKeys{GetAllKeys: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return nil, err
	}
	return resp.GetKeys().GetKeys(), nil
}

func (s *txObjectStore) Count(ctx context.Context, r *idb.KeyRange) (int64, error) {
	_ = ctx
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Count{Count: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return 0, err
	}
	return resp.GetCount().GetCount(), nil
}

func (s *txObjectStore) DeleteRange(ctx context.Context, r idb.KeyRange) (int64, error) {
	_ = ctx
	kr, err := krToProto(&r)
	if err != nil {
		return 0, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_DeleteRange{DeleteRange: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return 0, err
	}
	return resp.GetDelete().GetDeleted(), nil
}

func (s *txObjectStore) Index(name string) idb.TransactionIndex {
	return &txIndex{tx: s.tx, store: s.store, index: name}
}

// idb.TransactionIndex provides transaction-scoped index operations.
type txIndex struct {
	tx    *hostTx
	store string
	index string
}

func (idx *txIndex) Get(ctx context.Context, values ...any) (idb.Record, error) {
	_ = ctx
	req, err := idx.query(nil, values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGet{IndexGet: req}})
	if err != nil {
		return nil, err
	}
	record, err := idb.RecordFromProto(resp.GetRecord().GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (idx *txIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	_ = ctx
	req, err := idx.query(nil, values)
	if err != nil {
		return "", err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGetKey{IndexGetKey: req}})
	if err != nil {
		return "", err
	}
	return resp.GetKey().GetKey(), nil
}

func (idx *txIndex) GetAll(ctx context.Context, r *idb.KeyRange, values ...any) ([]idb.Record, error) {
	_ = ctx
	req, err := idx.query(r, values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGetAll{IndexGetAll: req}})
	if err != nil {
		return nil, err
	}
	records, err := idb.RecordsFromProto(resp.GetRecords().GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (idx *txIndex) GetAllKeys(ctx context.Context, r *idb.KeyRange, values ...any) ([]string, error) {
	_ = ctx
	req, err := idx.query(r, values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGetAllKeys{IndexGetAllKeys: req}})
	if err != nil {
		return nil, err
	}
	return resp.GetKeys().GetKeys(), nil
}

func (idx *txIndex) Count(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
	_ = ctx
	req, err := idx.query(r, values)
	if err != nil {
		return 0, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexCount{IndexCount: req}})
	if err != nil {
		return 0, err
	}
	return resp.GetCount().GetCount(), nil
}

func (idx *txIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	return idx.DeleteRange(ctx, nil, values...)
}

// DeleteRange removes all transaction-scoped rows that match values and r.
func (idx *txIndex) DeleteRange(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
	_ = ctx
	req, err := idx.query(r, values)
	if err != nil {
		return 0, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexDelete{IndexDelete: req}})
	if err != nil {
		return 0, err
	}
	return resp.GetDelete().GetDeleted(), nil
}

func (idx *txIndex) query(r *idb.KeyRange, values []any) (*proto.IndexQueryRequest, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	return &proto.IndexQueryRequest{Store: idx.store, Index: idx.index, Values: vals, Range: kr}, nil
}

// idb.Cursor streams IndexedDB rows one at a time.
type rpcCursor struct {
	stream      proto.IndexedDB_OpenCursorClient
	cancel      context.CancelFunc
	keysOnly    bool
	indexCursor bool
	entry       *proto.CursorEntry
	err         error
	done        bool
}

// Continue advances the cursor by one row.
func (c *rpcCursor) Continue() bool {
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_Next{Next: true},
	})
}

// ContinueToKey advances the cursor to the supplied key, or exhausts it if the
// key does not exist.
func (c *rpcCursor) ContinueToKey(key any) bool {
	kvs, err := idb.CursorKeyToProto(key, c.indexCursor)
	if err != nil {
		c.err = err
		return false
	}
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_ContinueToKey{ContinueToKey: &proto.CursorKeyTarget{Key: kvs}},
	})
}

// Advance skips count rows ahead.
func (c *rpcCursor) Advance(count int) bool {
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_Advance{Advance: int32(count)},
	})
}

// Key returns the current cursor key.
func (c *rpcCursor) Key() any {
	if c.entry == nil || len(c.entry.GetKey()) == 0 {
		return nil
	}
	parts, err := idb.KeyValuesToAny(c.entry.GetKey())
	if err != nil {
		c.err = err
		return nil
	}
	if !c.indexCursor && len(parts) == 1 {
		return parts[0]
	}
	return parts
}

// PrimaryKey returns the current record's primary key.
func (c *rpcCursor) PrimaryKey() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.GetPrimaryKey()
}

// Value returns the current record.
func (c *rpcCursor) Value() (idb.Record, error) {
	if c.keysOnly {
		return nil, idb.ErrKeysOnly
	}
	if c.entry == nil || c.entry.GetRecord() == nil {
		return nil, idb.ErrNotFound
	}
	return idb.RecordFromProto(c.entry.GetRecord())
}

// Delete removes the current row and keeps the cursor open.
func (c *rpcCursor) Delete() error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return idb.ErrNotFound
	}
	err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Delete{Delete: true},
			},
		},
	})
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	resp, err := c.stream.Recv()
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	if resp == nil {
		return c.setErr(fmt.Errorf("indexeddb: cursor stream ended during mutation"))
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
	case *proto.CursorResponse_Done:
		if v.Done {
			c.done = true
			c.entry = nil
		}
	default:
		return c.setErr(fmt.Errorf("indexeddb: unexpected cursor mutation ack"))
	}
	return nil
}

// Update replaces the current row and keeps the cursor open.
func (c *rpcCursor) Update(value idb.Record) error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return idb.ErrNotFound
	}
	pbRecord, err := idb.RecordToProto(value)
	if err != nil {
		return fmt.Errorf("indexeddb: marshal cursor update: %w", err)
	}
	err = c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Update{Update: pbRecord},
			},
		},
	})
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	resp, err := c.stream.Recv()
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	if resp == nil {
		return c.setErr(fmt.Errorf("indexeddb: cursor stream ended during mutation"))
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
	case *proto.CursorResponse_Done:
		if v.Done {
			c.done = true
			c.entry = nil
		} else if c.entry != nil {
			c.entry.Record = pbRecord
		}
	default:
		return c.setErr(fmt.Errorf("indexeddb: unexpected cursor mutation ack"))
	}
	return nil
}

// Err returns the terminal cursor error, if any.
func (c *rpcCursor) Err() error {
	return c.err
}

func (c *rpcCursor) cleanup() error {
	var err error
	if c.stream != nil {
		err = grpcErr(c.stream.CloseSend())
		c.stream = nil
	}
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return err
}

func (c *rpcCursor) setErr(err error) error {
	c.err = err
	_ = c.cleanup()
	return c.err
}

// Close closes the cursor stream and releases its transport resources.
func (c *rpcCursor) Close() error {
	c.done = true
	c.entry = nil
	if c.stream == nil {
		return c.cleanup()
	}
	sendErr := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Close{Close: true},
			},
		},
	})
	closeErr := c.cleanup()
	if sendErr != nil {
		return grpcErr(sendErr)
	}
	return closeErr
}

func (c *rpcCursor) sendAndRecv(cmd *proto.CursorCommand) bool {
	if c.done || c.err != nil {
		return false
	}
	err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: cmd},
	})
	if err != nil {
		_ = c.setErr(grpcErr(err))
		return false
	}
	resp, err := c.stream.Recv()
	if err != nil {
		_ = c.setErr(grpcErr(err))
		return false
	}
	if resp == nil {
		_ = c.setErr(fmt.Errorf("indexeddb: cursor stream ended"))
		return false
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
		return true
	case *proto.CursorResponse_Done:
		if !v.Done {
			_ = c.setErr(fmt.Errorf("indexeddb: unexpected non-exhaustion cursor ack"))
			c.entry = nil
			return false
		}
		c.done = true
		c.entry = nil
		return false
	default:
		_ = c.setErr(fmt.Errorf("indexeddb: unexpected cursor response"))
		c.entry = nil
		return false
	}
}

func cursorDirectionToProto(dir idb.CursorDirection) proto.CursorDirection {
	switch dir {
	case idb.CursorNextUnique:
		return proto.CursorDirection_CURSOR_NEXT_UNIQUE
	case idb.CursorPrev:
		return proto.CursorDirection_CURSOR_PREV
	case idb.CursorPrevUnique:
		return proto.CursorDirection_CURSOR_PREV_UNIQUE
	default:
		return proto.CursorDirection_CURSOR_NEXT
	}
}

func openCursor(ctx context.Context, client proto.IndexedDBClient, store, index string, r *idb.KeyRange, dir idb.CursorDirection, keysOnly bool, values []any) (idb.Cursor, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	vals, err := idb.TypedValuesFromAny(values)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, streamCancel := context.WithCancel(ctx)
	stream, err := client.OpenCursor(streamCtx)
	if err != nil {
		streamCancel()
		return nil, grpcErr(err)
	}
	err = stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Open{
			Open: &proto.OpenCursorRequest{
				Store:     store,
				Range:     kr,
				Direction: cursorDirectionToProto(dir),
				KeysOnly:  keysOnly,
				Index:     index,
				Values:    vals,
			},
		},
	})
	if err != nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, grpcErr(err)
	}
	// Read the open ack to surface creation errors synchronously.
	resp, err := stream.Recv()
	if err != nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, grpcErr(err)
	}
	if resp == nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, fmt.Errorf("indexeddb: cursor stream ended during open")
	}
	done, ok := resp.GetResult().(*proto.CursorResponse_Done)
	if !ok || done.Done {
		_ = stream.CloseSend()
		streamCancel()
		return nil, fmt.Errorf("indexeddb: unexpected cursor open ack")
	}
	return &rpcCursor{stream: stream, cancel: streamCancel, keysOnly: keysOnly, indexCursor: index != ""}, nil
}

func krToProto(r *idb.KeyRange) (*proto.KeyRange, error) {
	if r == nil {
		return nil, nil
	}
	kr := &proto.KeyRange{LowerOpen: r.LowerOpen, UpperOpen: r.UpperOpen}
	if r.Lower != nil {
		v, err := idb.TypedValueFromAny(r.Lower)
		if err != nil {
			return nil, fmt.Errorf("marshal key range lower: %w", err)
		}
		kr.Lower = v
	}
	if r.Upper != nil {
		v, err := idb.TypedValueFromAny(r.Upper)
		if err != nil {
			return nil, fmt.Errorf("marshal key range upper: %w", err)
		}
		kr.Upper = v
	}
	return kr, nil
}

func anyToProtoValues(values []any) ([]*proto.TypedValue, error) {
	return idb.TypedValuesFromAny(values)
}

func transactionModeToProto(mode idb.TransactionMode) proto.TransactionMode {
	if mode == idb.TransactionReadwrite {
		return proto.TransactionMode_TRANSACTION_READWRITE
	}
	return proto.TransactionMode_TRANSACTION_READONLY
}

func durabilityHintToProto(hint idb.TransactionDurabilityHint) proto.TransactionDurabilityHint {
	switch hint {
	case idb.TransactionDurabilityStrict:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_STRICT
	case idb.TransactionDurabilityRelaxed:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_RELAXED
	default:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_DEFAULT
	}
}

func rpcStatusErr(st *rpcstatus.Status) error {
	if st == nil || st.GetCode() == int32(codes.OK) {
		return nil
	}
	return grpcErr(status.Error(codes.Code(st.GetCode()), st.GetMessage()))
}

func grpcErr(err error) error {
	return idb.RPCError(err)
}
