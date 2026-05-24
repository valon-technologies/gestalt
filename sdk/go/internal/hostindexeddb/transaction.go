package hostindexeddb

import (
	"context"
	"fmt"
	"sync"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
)

// hostTransaction is an explicit IndexedDB transaction over a fixed store scope.
type hostTransaction struct {
	stream proto.IndexedDB_TransactionClient
	cancel context.CancelFunc
	mu     sync.Mutex
	nextID uint64
	done   bool
	err    error
}

// ObjectStore returns a transaction-scoped object store handle.
func (tx *hostTransaction) ObjectStore(name string) idb.TransactionObjectStore {
	return &hostTxObjectStore{tx: tx, store: name}
}

// Commit atomically commits all writes made in the transaction.
func (tx *hostTransaction) Commit(ctx context.Context) error {
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
func (tx *hostTransaction) Abort(ctx context.Context) error {
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

func (tx *hostTransaction) sendOperation(op *proto.TransactionOperation) (*proto.TransactionOperationResponse, error) {
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

func (tx *hostTransaction) failLocked(err error) error {
	tx.err = err
	tx.done = true
	tx.mu.Unlock()
	tx.cleanup()
	return err
}

func (tx *hostTransaction) cleanup() {
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
type hostTxObjectStore struct {
	tx    *hostTransaction
	store string
}

func (s *hostTxObjectStore) Get(ctx context.Context, id string) (idb.Record, error) {
	_ = ctx
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Get{Get: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	if err != nil {
		return nil, err
	}
	record, err := recordFromProto(resp.GetRecord().GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (s *hostTxObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	_ = ctx
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetKey{GetKey: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	if err != nil {
		return "", err
	}
	return resp.GetKey().GetKey(), nil
}

func (s *hostTxObjectStore) Add(ctx context.Context, record idb.Record) error {
	_ = ctx
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Add{Add: &proto.RecordRequest{Store: s.store, Record: pbRecord}}})
	return err
}

func (s *hostTxObjectStore) Put(ctx context.Context, record idb.Record) error {
	_ = ctx
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Put{Put: &proto.RecordRequest{Store: s.store, Record: pbRecord}}})
	return err
}

func (s *hostTxObjectStore) Delete(ctx context.Context, id string) error {
	_ = ctx
	_, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Delete{Delete: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	return err
}

func (s *hostTxObjectStore) Clear(ctx context.Context) error {
	_ = ctx
	_, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Clear{Clear: &proto.ObjectStoreNameRequest{Store: s.store}}})
	return err
}

func (s *hostTxObjectStore) GetAll(ctx context.Context, r *idb.KeyRange) ([]idb.Record, error) {
	_ = ctx
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetAll{GetAll: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return nil, err
	}
	records, err := recordsFromProto(resp.GetRecords().GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (s *hostTxObjectStore) GetAllKeys(ctx context.Context, r *idb.KeyRange) ([]string, error) {
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

func (s *hostTxObjectStore) Count(ctx context.Context, r *idb.KeyRange) (int64, error) {
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

func (s *hostTxObjectStore) DeleteRange(ctx context.Context, r idb.KeyRange) (int64, error) {
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

func (s *hostTxObjectStore) Index(name string) idb.TransactionIndex {
	return &hostTxIndex{tx: s.tx, store: s.store, index: name}
}

// idb.TransactionIndex provides transaction-scoped index operations.
type hostTxIndex struct {
	tx    *hostTransaction
	store string
	index string
}

func (idx *hostTxIndex) Get(ctx context.Context, values ...any) (idb.Record, error) {
	_ = ctx
	req, err := idx.query(nil, values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGet{IndexGet: req}})
	if err != nil {
		return nil, err
	}
	record, err := recordFromProto(resp.GetRecord().GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (idx *hostTxIndex) GetKey(ctx context.Context, values ...any) (string, error) {
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

func (idx *hostTxIndex) GetAll(ctx context.Context, r *idb.KeyRange, values ...any) ([]idb.Record, error) {
	_ = ctx
	req, err := idx.query(r, values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGetAll{IndexGetAll: req}})
	if err != nil {
		return nil, err
	}
	records, err := recordsFromProto(resp.GetRecords().GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (idx *hostTxIndex) GetAllKeys(ctx context.Context, r *idb.KeyRange, values ...any) ([]string, error) {
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

func (idx *hostTxIndex) Count(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
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

func (idx *hostTxIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	return idx.DeleteRange(ctx, nil, values...)
}

// DeleteRange removes all transaction-scoped rows that match values and r.
func (idx *hostTxIndex) DeleteRange(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
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

func (idx *hostTxIndex) query(r *idb.KeyRange, values []any) (*proto.IndexQueryRequest, error) {
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

// Cursor streams IndexedDB rows one at a time.
// HostCursor is the host-service cursor implementation.
type HostCursor = hostCursor
