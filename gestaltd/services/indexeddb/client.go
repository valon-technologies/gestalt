package indexeddb

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecConfig struct {
	Command    string
	Args       []string
	Env        map[string]string
	Config     map[string]any
	Egress     egress.Policy
	HostBinary string
	Cleanup    func()
	Name       string
}

type remoteIndexedDB struct {
	client  proto.IndexedDBClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (coreindexeddb.IndexedDB, error) {
	proc, err := runtimehost.StartPluginProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		ProviderName: cfg.Name,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proc.Lifecycle()
	dsClient := proto.NewIndexedDBClient(proc.Conn())

	_, err = runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_INDEXEDDB, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteIndexedDB{client: dsClient, runtime: runtimeClient, closer: proc}, nil
}

func (r *remoteIndexedDB) ObjectStore(name string) coreindexeddb.ObjectStore {
	return &remoteObjectStore{client: r.client, store: name}
}

func (r *remoteIndexedDB) Transaction(ctx context.Context, stores []string, mode coreindexeddb.TransactionMode, opts coreindexeddb.TransactionOptions) (coreindexeddb.Transaction, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := r.client.Transaction(streamCtx)
	if err != nil {
		cancel()
		return nil, grpcToDatastoreErr(err)
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
		return nil, grpcToDatastoreErr(err)
	}
	resp, err := stream.Recv()
	if err != nil {
		_ = stream.CloseSend()
		cancel()
		return nil, grpcToDatastoreErr(err)
	}
	if resp.GetBegin() == nil {
		_ = stream.CloseSend()
		cancel()
		return nil, fmt.Errorf("indexeddb transaction: expected begin response")
	}
	return &remoteTransaction{stream: stream, cancel: cancel}, nil
}

func (r *remoteIndexedDB) CreateObjectStore(ctx context.Context, name string, schema coreindexeddb.ObjectStoreSchema) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	indexes := make([]*proto.IndexSchema, len(schema.Indexes))
	for i, idx := range schema.Indexes {
		indexes[i] = &proto.IndexSchema{Name: idx.Name, KeyPath: idx.KeyPath, Unique: idx.Unique}
	}
	columns := make([]*proto.ColumnDef, len(schema.Columns))
	for i, col := range schema.Columns {
		columns[i] = &proto.ColumnDef{
			Name: col.Name, Type: int32(col.Type),
			PrimaryKey: col.PrimaryKey, NotNull: col.NotNull, Unique: col.Unique,
		}
	}
	_, err := r.client.CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{
		Name: name, Schema: &proto.ObjectStoreSchema{Indexes: indexes, Columns: columns},
	})
	return grpcToDatastoreErr(err)
}

func (r *remoteIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.client.DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{Name: name})
	return grpcToDatastoreErr(err)
}

func (r *remoteIndexedDB) Ping(ctx context.Context) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteIndexedDB) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

// --- ObjectStore ---

type remoteObjectStore struct {
	client proto.IndexedDBClient
	store  string
}

func (o *remoteObjectStore) Get(ctx context.Context, id string) (coreindexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := o.client.Get(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	record, err := gestalt.RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (o *remoteObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := o.client.GetKey(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return "", grpcToDatastoreErr(err)
	}
	return resp.GetKey(), nil
}

func (o *remoteObjectStore) Add(ctx context.Context, record coreindexeddb.Record) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbRecord, err := gestalt.RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Add(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) Put(ctx context.Context, record coreindexeddb.Record) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbRecord, err := gestalt.RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Put(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) Delete(ctx context.Context, id string) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := o.client.Delete(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) Clear(ctx context.Context) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := o.client.Clear(ctx, &proto.ObjectStoreNameRequest{Store: o.store})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) GetAll(ctx context.Context, r *coreindexeddb.KeyRange) ([]coreindexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAll(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	records, err := gestalt.RecordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (o *remoteObjectStore) GetAllKeys(ctx context.Context, r *coreindexeddb.KeyRange) ([]string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAllKeys(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return resp.GetKeys(), nil
}

func (o *remoteObjectStore) Count(ctx context.Context, r *coreindexeddb.KeyRange) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.Count(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetCount(), nil
}

func (o *remoteObjectStore) DeleteRange(ctx context.Context, r coreindexeddb.KeyRange) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(&r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.DeleteRange(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetDeleted(), nil
}

func (o *remoteObjectStore) Index(name string) coreindexeddb.Index {
	return &remoteIndex{client: o.client, store: o.store, index: name}
}

func (o *remoteObjectStore) OpenCursor(ctx context.Context, r *coreindexeddb.KeyRange, dir coreindexeddb.CursorDirection) (coreindexeddb.Cursor, error) {
	return openRemoteCursor(ctx, o.client, o.store, "", r, dir, false, nil)
}

func (o *remoteObjectStore) OpenKeyCursor(ctx context.Context, r *coreindexeddb.KeyRange, dir coreindexeddb.CursorDirection) (coreindexeddb.Cursor, error) {
	return openRemoteCursor(ctx, o.client, o.store, "", r, dir, true, nil)
}

// --- Index ---

type remoteIndex struct {
	client proto.IndexedDBClient
	store  string
	index  string
}

func (idx *remoteIndex) Get(ctx context.Context, values ...any) (coreindexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGet(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues,
	})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	record, err := gestalt.RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (idx *remoteIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return "", err
	}
	resp, err := idx.client.IndexGetKey(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues,
	})
	if err != nil {
		return "", grpcToDatastoreErr(err)
	}
	return resp.GetKey(), nil
}

func (idx *remoteIndex) GetAll(ctx context.Context, r *coreindexeddb.KeyRange, values ...any) ([]coreindexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAll(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	records, err := gestalt.RecordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (idx *remoteIndex) GetAllKeys(ctx context.Context, r *coreindexeddb.KeyRange, values ...any) ([]string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAllKeys(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return resp.GetKeys(), nil
}

func (idx *remoteIndex) Count(ctx context.Context, r *coreindexeddb.KeyRange, values ...any) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return 0, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexCount(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetCount(), nil
}

func (idx *remoteIndex) OpenCursor(ctx context.Context, r *coreindexeddb.KeyRange, dir coreindexeddb.CursorDirection, values ...any) (coreindexeddb.Cursor, error) {
	return openRemoteCursor(ctx, idx.client, idx.store, idx.index, r, dir, false, values)
}

func (idx *remoteIndex) OpenKeyCursor(ctx context.Context, r *coreindexeddb.KeyRange, dir coreindexeddb.CursorDirection, values ...any) (coreindexeddb.Cursor, error) {
	return openRemoteCursor(ctx, idx.client, idx.store, idx.index, r, dir, true, values)
}

func (idx *remoteIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexDelete(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues,
	})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetDeleted(), nil
}

// --- Transaction ---

type remoteTransaction struct {
	stream proto.IndexedDB_TransactionClient
	cancel context.CancelFunc
	mu     sync.Mutex
	nextID uint64
	done   bool
	err    error
}

func (tx *remoteTransaction) ObjectStore(name string) coreindexeddb.TransactionObjectStore {
	return &remoteTransactionObjectStore{tx: tx, store: name}
}

func (tx *remoteTransaction) Commit(context.Context) error {
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return err
		}
		return coreindexeddb.ErrTransactionDone
	}
	if tx.err != nil {
		err := tx.err
		tx.mu.Unlock()
		return err
	}
	tx.done = true

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Commit{Commit: &proto.TransactionCommitRequest{}}}); err != nil {
		return tx.failLocked(grpcToDatastoreErr(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return tx.failLocked(grpcToDatastoreErr(err))
	}
	commit := resp.GetCommit()
	if commit == nil {
		return tx.failLocked(fmt.Errorf("indexeddb transaction: expected commit response"))
	}
	if err := rpcStatusToDatastoreErr(commit.GetError()); err != nil {
		return tx.failLocked(err)
	}
	tx.mu.Unlock()
	tx.cleanup()
	return nil
}

func (tx *remoteTransaction) Abort(context.Context) error {
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return err
		}
		return coreindexeddb.ErrTransactionDone
	}
	tx.done = true

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Abort{Abort: &proto.TransactionAbortRequest{}}}); err != nil {
		return tx.failLocked(grpcToDatastoreErr(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return tx.failLocked(grpcToDatastoreErr(err))
	}
	abort := resp.GetAbort()
	if abort == nil {
		return tx.failLocked(fmt.Errorf("indexeddb transaction: expected abort response"))
	}
	if err := rpcStatusToDatastoreErr(abort.GetError()); err != nil {
		return tx.failLocked(err)
	}
	tx.mu.Unlock()
	tx.cleanup()
	return nil
}

func (tx *remoteTransaction) sendOperation(op *proto.TransactionOperation) (*proto.TransactionOperationResponse, error) {
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, coreindexeddb.ErrTransactionDone
	}
	if tx.err != nil {
		err := tx.err
		tx.mu.Unlock()
		return nil, err
	}
	tx.nextID++
	op.RequestId = tx.nextID

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Operation{Operation: op}}); err != nil {
		return nil, tx.failLocked(grpcToDatastoreErr(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return nil, tx.failLocked(grpcToDatastoreErr(err))
	}
	opResp := resp.GetOperation()
	if opResp == nil {
		return nil, tx.failLocked(fmt.Errorf("indexeddb transaction: expected operation response"))
	}
	if opResp.GetRequestId() != op.GetRequestId() {
		return nil, tx.failLocked(fmt.Errorf("indexeddb transaction: response request id %d does not match %d", opResp.GetRequestId(), op.GetRequestId()))
	}
	if err := rpcStatusToDatastoreErr(opResp.GetError()); err != nil {
		tx.done = true
		tx.err = err
		tx.mu.Unlock()
		tx.cleanup()
		return nil, err
	}
	tx.mu.Unlock()
	return opResp, nil
}

func (tx *remoteTransaction) failLocked(err error) error {
	tx.err = err
	tx.done = true
	tx.mu.Unlock()
	tx.cleanup()
	return err
}

func (tx *remoteTransaction) cleanup() {
	if tx.stream != nil {
		_ = tx.stream.CloseSend()
		tx.stream = nil
	}
	if tx.cancel != nil {
		tx.cancel()
		tx.cancel = nil
	}
}

type remoteTransactionObjectStore struct {
	tx    *remoteTransaction
	store string
}

func (s *remoteTransactionObjectStore) Get(_ context.Context, id string) (coreindexeddb.Record, error) {
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Get{Get: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	if err != nil {
		return nil, err
	}
	record, err := gestalt.RecordFromProto(resp.GetRecord().GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (s *remoteTransactionObjectStore) GetKey(_ context.Context, id string) (string, error) {
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetKey{GetKey: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	if err != nil {
		return "", err
	}
	return resp.GetKey().GetKey(), nil
}

func (s *remoteTransactionObjectStore) Add(_ context.Context, record coreindexeddb.Record) error {
	pbRecord, err := gestalt.RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Add{Add: &proto.RecordRequest{Store: s.store, Record: pbRecord}}})
	return err
}

func (s *remoteTransactionObjectStore) Put(_ context.Context, record coreindexeddb.Record) error {
	pbRecord, err := gestalt.RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Put{Put: &proto.RecordRequest{Store: s.store, Record: pbRecord}}})
	return err
}

func (s *remoteTransactionObjectStore) Delete(_ context.Context, id string) error {
	_, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Delete{Delete: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	return err
}

func (s *remoteTransactionObjectStore) Clear(context.Context) error {
	_, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Clear{Clear: &proto.ObjectStoreNameRequest{Store: s.store}}})
	return err
}

func (s *remoteTransactionObjectStore) GetAll(_ context.Context, r *coreindexeddb.KeyRange) ([]coreindexeddb.Record, error) {
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetAll{GetAll: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return nil, err
	}
	records, err := gestalt.RecordsFromProto(resp.GetRecords().GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (s *remoteTransactionObjectStore) GetAllKeys(_ context.Context, r *coreindexeddb.KeyRange) ([]string, error) {
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetAllKeys{GetAllKeys: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return nil, err
	}
	return resp.GetKeys().GetKeys(), nil
}

func (s *remoteTransactionObjectStore) Count(_ context.Context, r *coreindexeddb.KeyRange) (int64, error) {
	kr, err := keyRangeToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Count{Count: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return 0, err
	}
	return resp.GetCount().GetCount(), nil
}

func (s *remoteTransactionObjectStore) DeleteRange(_ context.Context, r coreindexeddb.KeyRange) (int64, error) {
	kr, err := keyRangeToProto(&r)
	if err != nil {
		return 0, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_DeleteRange{DeleteRange: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return 0, err
	}
	return resp.GetDelete().GetDeleted(), nil
}

func (s *remoteTransactionObjectStore) Index(name string) coreindexeddb.TransactionIndex {
	return &remoteTransactionIndex{tx: s.tx, store: s.store, index: name}
}

type remoteTransactionIndex struct {
	tx    *remoteTransaction
	store string
	index string
}

func (idx *remoteTransactionIndex) Get(_ context.Context, values ...any) (coreindexeddb.Record, error) {
	req, err := idx.query(nil, values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGet{IndexGet: req}})
	if err != nil {
		return nil, err
	}
	record, err := gestalt.RecordFromProto(resp.GetRecord().GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (idx *remoteTransactionIndex) GetKey(_ context.Context, values ...any) (string, error) {
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

func (idx *remoteTransactionIndex) GetAll(_ context.Context, r *coreindexeddb.KeyRange, values ...any) ([]coreindexeddb.Record, error) {
	req, err := idx.query(r, values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGetAll{IndexGetAll: req}})
	if err != nil {
		return nil, err
	}
	records, err := gestalt.RecordsFromProto(resp.GetRecords().GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (idx *remoteTransactionIndex) GetAllKeys(_ context.Context, r *coreindexeddb.KeyRange, values ...any) ([]string, error) {
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

func (idx *remoteTransactionIndex) Count(_ context.Context, r *coreindexeddb.KeyRange, values ...any) (int64, error) {
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

func (idx *remoteTransactionIndex) Delete(_ context.Context, values ...any) (int64, error) {
	req, err := idx.query(nil, values)
	if err != nil {
		return 0, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexDelete{IndexDelete: req}})
	if err != nil {
		return 0, err
	}
	return resp.GetDelete().GetDeleted(), nil
}

func (idx *remoteTransactionIndex) query(r *coreindexeddb.KeyRange, values []any) (*proto.IndexQueryRequest, error) {
	pbValues, err := toProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	return &proto.IndexQueryRequest{Store: idx.store, Index: idx.index, Values: pbValues, Range: kr}, nil
}

// --- Helpers ---

func toProtoValues(values []any) ([]*proto.TypedValue, error) {
	return gestalt.TypedValuesFromAny(values)
}

func transactionModeToProto(mode coreindexeddb.TransactionMode) proto.TransactionMode {
	if mode == coreindexeddb.TransactionReadwrite {
		return proto.TransactionMode_TRANSACTION_READWRITE
	}
	return proto.TransactionMode_TRANSACTION_READONLY
}

func durabilityHintToProto(hint coreindexeddb.TransactionDurabilityHint) proto.TransactionDurabilityHint {
	switch hint {
	case coreindexeddb.TransactionDurabilityStrict:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_STRICT
	case coreindexeddb.TransactionDurabilityRelaxed:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_RELAXED
	default:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_DEFAULT
	}
}

func rpcStatusToDatastoreErr(st *rpcstatus.Status) error {
	if st == nil || st.GetCode() == int32(codes.OK) {
		return nil
	}
	return grpcToDatastoreErr(status.Error(codes.Code(st.GetCode()), st.GetMessage()))
}

func keyRangeToProto(r *coreindexeddb.KeyRange) (*proto.KeyRange, error) {
	if r == nil {
		return nil, nil
	}
	kr := &proto.KeyRange{
		LowerOpen: r.LowerOpen,
		UpperOpen: r.UpperOpen,
	}
	if r.Lower != nil {
		v, err := gestalt.TypedValueFromAny(r.Lower)
		if err != nil {
			return nil, fmt.Errorf("marshal key range lower: %w", err)
		}
		kr.Lower = v
	}
	if r.Upper != nil {
		v, err := gestalt.TypedValueFromAny(r.Upper)
		if err != nil {
			return nil, fmt.Errorf("marshal key range upper: %w", err)
		}
		kr.Upper = v
	}
	return kr, nil
}

func grpcToDatastoreErr(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return coreindexeddb.ErrNotFound
	case codes.AlreadyExists:
		return coreindexeddb.ErrAlreadyExists
	case codes.InvalidArgument:
		if strings.Contains(st.Message(), "invalid transaction") {
			return coreindexeddb.ErrInvalidTransaction
		}
		return err
	case codes.FailedPrecondition:
		if strings.Contains(st.Message(), "readonly") {
			return coreindexeddb.ErrReadOnly
		}
		if strings.Contains(st.Message(), "already finished") {
			return coreindexeddb.ErrTransactionDone
		}
		return err
	default:
		return err
	}
}

// --- Remote Cursor ---

func cursorDirectionToProto(dir coreindexeddb.CursorDirection) proto.CursorDirection {
	switch dir {
	case coreindexeddb.CursorNextUnique:
		return proto.CursorDirection_CURSOR_NEXT_UNIQUE
	case coreindexeddb.CursorPrev:
		return proto.CursorDirection_CURSOR_PREV
	case coreindexeddb.CursorPrevUnique:
		return proto.CursorDirection_CURSOR_PREV_UNIQUE
	default:
		return proto.CursorDirection_CURSOR_NEXT
	}
}

func openRemoteCursor(ctx context.Context, client proto.IndexedDBClient, store, index string, r *coreindexeddb.KeyRange, dir coreindexeddb.CursorDirection, keysOnly bool, values []any) (*remoteCursor, error) {
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	var pbValues []*proto.TypedValue
	if len(values) > 0 {
		pbValues, err = toProtoValues(values)
		if err != nil {
			return nil, err
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, streamCancel := context.WithCancel(ctx)
	stream, err := client.OpenCursor(streamCtx)
	if err != nil {
		streamCancel()
		return nil, grpcToDatastoreErr(err)
	}
	if err := stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Open{Open: &proto.OpenCursorRequest{
			Store:     store,
			Range:     kr,
			Direction: cursorDirectionToProto(dir),
			KeysOnly:  keysOnly,
			Index:     index,
			Values:    pbValues,
		}},
	}); err != nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, grpcToDatastoreErr(err)
	}
	// Read the open ack to surface creation errors synchronously.
	resp, err := stream.Recv()
	if err != nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, grpcToDatastoreErr(err)
	}
	if resp == nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, fmt.Errorf("cursor stream ended during open")
	}
	done, ok := resp.GetResult().(*proto.CursorResponse_Done)
	if !ok || done.Done {
		_ = stream.CloseSend()
		streamCancel()
		return nil, fmt.Errorf("unexpected cursor open ack")
	}
	return &remoteCursor{stream: stream, cancel: streamCancel, keysOnly: keysOnly, indexCursor: index != ""}, nil
}

type remoteCursor struct {
	stream      proto.IndexedDB_OpenCursorClient
	cancel      context.CancelFunc
	keysOnly    bool
	indexCursor bool
	entry       *proto.CursorEntry
	err         error
	done        bool
}

func (c *remoteCursor) cleanup() {
	if c.stream != nil {
		_ = c.stream.CloseSend()
		c.stream = nil
	}
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
}

func (c *remoteCursor) setErr(err error) error {
	c.err = err
	c.cleanup()
	return c.err
}

func (c *remoteCursor) sendAndRecv(msg *proto.CursorClientMessage) bool {
	if c.done || c.err != nil {
		return false
	}
	if err := c.stream.Send(msg); err != nil {
		c.err = grpcToDatastoreErr(err)
		c.cleanup()
		return false
	}
	resp, err := c.stream.Recv()
	if err != nil {
		c.err = grpcToDatastoreErr(err)
		c.cleanup()
		return false
	}
	if resp == nil {
		c.err = fmt.Errorf("cursor stream ended")
		c.cleanup()
		return false
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
		return true
	case *proto.CursorResponse_Done:
		if !v.Done {
			_ = c.setErr(fmt.Errorf("unexpected non-exhaustion cursor ack"))
			return false
		}
		c.done = true
		c.entry = nil
		return false
	default:
		_ = c.setErr(fmt.Errorf("unexpected cursor response"))
		return false
	}
}

func (c *remoteCursor) Continue() bool {
	return c.sendAndRecv(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Next{Next: true},
		}},
	})
}

func (c *remoteCursor) ContinueToKey(key any) bool {
	kvs, err := gestalt.CursorKeyToProto(key, c.indexCursor)
	if err != nil {
		c.err = err
		return false
	}
	return c.sendAndRecv(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_ContinueToKey{ContinueToKey: &proto.CursorKeyTarget{Key: kvs}},
		}},
	})
}

func (c *remoteCursor) Advance(count int) bool {
	return c.sendAndRecv(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Advance{Advance: int32(count)},
		}},
	})
}

func (c *remoteCursor) Key() any {
	if c.entry == nil || len(c.entry.Key) == 0 {
		return nil
	}
	parts, err := gestalt.KeyValuesToAny(c.entry.Key)
	if err != nil {
		c.err = err
		return nil
	}
	if !c.indexCursor && len(parts) == 1 {
		return parts[0]
	}
	return parts
}

func (c *remoteCursor) PrimaryKey() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.PrimaryKey
}

func (c *remoteCursor) Value() (coreindexeddb.Record, error) {
	if c.keysOnly {
		return nil, coreindexeddb.ErrKeysOnly
	}
	if c.entry == nil || c.entry.Record == nil {
		return nil, coreindexeddb.ErrNotFound
	}
	return gestalt.RecordFromProto(c.entry.Record)
}

func (c *remoteCursor) Delete() error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return coreindexeddb.ErrNotFound
	}
	if err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Delete{Delete: true},
		}},
	}); err != nil {
		return c.setErr(grpcToDatastoreErr(err))
	}
	resp, err := c.stream.Recv()
	if err != nil {
		return c.setErr(grpcToDatastoreErr(err))
	}
	if resp == nil {
		return c.setErr(fmt.Errorf("cursor stream ended during mutation"))
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
		return c.setErr(fmt.Errorf("unexpected cursor mutation ack"))
	}
	return nil
}

func (c *remoteCursor) Update(value coreindexeddb.Record) error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return coreindexeddb.ErrNotFound
	}
	pbRec, err := gestalt.RecordToProto(value)
	if err != nil {
		return fmt.Errorf("marshal update record: %w", err)
	}
	if err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Update{Update: pbRec},
		}},
	}); err != nil {
		return c.setErr(grpcToDatastoreErr(err))
	}
	resp, err := c.stream.Recv()
	if err != nil {
		return c.setErr(grpcToDatastoreErr(err))
	}
	if resp == nil {
		return c.setErr(fmt.Errorf("cursor stream ended during mutation"))
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
	case *proto.CursorResponse_Done:
		if v.Done {
			c.done = true
			c.entry = nil
		} else if c.entry != nil {
			c.entry.Record = pbRec
		}
	default:
		return c.setErr(fmt.Errorf("unexpected cursor mutation ack"))
	}
	return nil
}

func (c *remoteCursor) Err() error { return c.err }

func (c *remoteCursor) Close() error {
	if c.stream == nil {
		return nil
	}
	c.done = true
	c.entry = nil
	_ = c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Close{Close: true},
		}},
	})
	c.cleanup()
	return nil
}

var _ coreindexeddb.IndexedDB = (*remoteIndexedDB)(nil)
