package indexeddb

import (
	"context"
	"fmt"
	"sync"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
)

type remoteTransaction struct {
	stream proto.IndexedDB_TransactionClient
	cancel context.CancelFunc
	mu     sync.Mutex
	nextID uint64
	done   bool
	err    error
}

func (tx *remoteTransaction) ObjectStore(name string) idb.TransactionObjectStore {
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
		return idb.ErrTransactionDone
	}
	if tx.err != nil {
		err := tx.err
		tx.mu.Unlock()
		return err
	}
	tx.done = true

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Commit{Commit: &proto.TransactionCommitRequest{}}}); err != nil {
		return tx.failLocked(idb.RPCError(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return tx.failLocked(idb.RPCError(err))
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
		return idb.ErrTransactionDone
	}
	tx.done = true

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Abort{Abort: &proto.TransactionAbortRequest{}}}); err != nil {
		return tx.failLocked(idb.RPCError(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return tx.failLocked(idb.RPCError(err))
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
		return nil, tx.failLocked(idb.RPCError(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return nil, tx.failLocked(idb.RPCError(err))
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

func (s *remoteTransactionObjectStore) Get(_ context.Context, id string) (idb.Record, error) {
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

func (s *remoteTransactionObjectStore) GetKey(_ context.Context, id string) (string, error) {
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetKey{GetKey: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	if err != nil {
		return "", err
	}
	return resp.GetKey().GetKey(), nil
}

func (s *remoteTransactionObjectStore) Add(_ context.Context, record idb.Record) error {
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Add{Add: &proto.RecordRequest{Store: s.store, Record: pbRecord}}})
	return err
}

func (s *remoteTransactionObjectStore) Put(_ context.Context, record idb.Record) error {
	pbRecord, err := recordToProto(record)
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

func (s *remoteTransactionObjectStore) GetAll(_ context.Context, r *idb.KeyRange) ([]idb.Record, error) {
	kr, err := keyRangeToProto(r)
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

func (s *remoteTransactionObjectStore) GetAllKeys(_ context.Context, r *idb.KeyRange) ([]string, error) {
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

func (s *remoteTransactionObjectStore) Count(_ context.Context, r *idb.KeyRange) (int64, error) {
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

func (s *remoteTransactionObjectStore) DeleteRange(_ context.Context, r idb.KeyRange) (int64, error) {
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

func (s *remoteTransactionObjectStore) Index(name string) idb.TransactionIndex {
	return &remoteTransactionIndex{tx: s.tx, store: s.store, index: name}
}

type remoteTransactionIndex struct {
	tx    *remoteTransaction
	store string
	index string
}

func (idx *remoteTransactionIndex) Get(_ context.Context, values ...any) (idb.Record, error) {
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

func (idx *remoteTransactionIndex) GetAll(_ context.Context, r *idb.KeyRange, values ...any) ([]idb.Record, error) {
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

func (idx *remoteTransactionIndex) GetAllKeys(_ context.Context, r *idb.KeyRange, values ...any) ([]string, error) {
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

func (idx *remoteTransactionIndex) Count(_ context.Context, r *idb.KeyRange, values ...any) (int64, error) {
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

func (idx *remoteTransactionIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	return idx.DeleteRange(ctx, nil, values...)
}

func (idx *remoteTransactionIndex) DeleteRange(_ context.Context, r *idb.KeyRange, values ...any) (int64, error) {
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

func (idx *remoteTransactionIndex) query(r *idb.KeyRange, values []any) (*proto.IndexQueryRequest, error) {
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
