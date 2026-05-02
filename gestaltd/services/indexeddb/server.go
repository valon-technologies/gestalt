package indexeddb

import (
	"context"
	"errors"
	"fmt"
	"io"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type indexedDBServer struct {
	proto.UnimplementedIndexedDBServer
	ds      coreindexeddb.IndexedDB
	db      string
	plugin  string
	allowed map[string]struct{}
}

type ServerOptions struct {
	AllowedStores []string
}

func NewServer(ds coreindexeddb.IndexedDB, pluginName string, opts ServerOptions) proto.IndexedDBServer {
	allowed := make(map[string]struct{}, len(opts.AllowedStores))
	for _, store := range opts.AllowedStores {
		allowed[store] = struct{}{}
	}
	if len(allowed) == 0 {
		allowed = nil
	}
	return &indexedDBServer{
		ds:      ds,
		db:      metricutil.IndexedDBName(ds),
		plugin:  pluginName,
		allowed: allowed,
	}
}

func (s *indexedDBServer) storeName(name string) string {
	return name
}

func (s *indexedDBServer) ensureAllowedStore(name string) error {
	if len(s.allowed) == 0 {
		return nil
	}
	if _, ok := s.allowed[name]; ok {
		return nil
	}
	return coreindexeddb.ErrNotFound
}

func (s *indexedDBServer) objectStore(name string) (coreindexeddb.ObjectStore, error) {
	if err := s.ensureAllowedStore(name); err != nil {
		return nil, err
	}
	return metricutil.InstrumentObjectStore(
		metricutil.UnwrapIndexedDB(s.ds).ObjectStore(s.storeName(name)),
		metricutil.IndexedDBMetricLabels{
			DB:           s.db,
			ProviderName: s.plugin,
			ObjectStore:  name,
		},
	), nil
}

func (s *indexedDBServer) CreateObjectStore(ctx context.Context, req *proto.CreateObjectStoreRequest) (*emptypb.Empty, error) {
	if err := s.ensureAllowedStore(req.GetName()); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	schema := protoToSchema(req.GetSchema())
	if err := s.ds.CreateObjectStore(ctx, s.storeName(req.GetName()), schema); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) DeleteObjectStore(ctx context.Context, req *proto.DeleteObjectStoreRequest) (*emptypb.Empty, error) {
	if err := s.ensureAllowedStore(req.GetName()); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := s.ds.DeleteObjectStore(ctx, s.storeName(req.GetName())); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Get(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	rec, err := store.Get(ctx, req.GetId())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordResponseFromRecord(rec)
}

func (s *indexedDBServer) GetKey(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.KeyResponse, error) {
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	key, err := store.GetKey(ctx, req.GetId())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBServer) Add(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	record, err := recordFromProto(req.GetRecord())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal record: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Add(ctx, record); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Put(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	record, err := recordFromProto(req.GetRecord())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal record: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Put(ctx, record); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Delete(ctx context.Context, req *proto.ObjectStoreRequest) (*emptypb.Empty, error) {
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Delete(ctx, req.GetId()); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Clear(ctx context.Context, req *proto.ObjectStoreNameRequest) (*emptypb.Empty, error) {
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Clear(ctx); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) GetAll(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.RecordsResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	recs, err := store.GetAll(ctx, keyRange)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordsResponseFromRecords(recs)
}

func (s *indexedDBServer) GetAllKeys(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.KeysResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	keys, err := store.GetAllKeys(ctx, keyRange)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBServer) Count(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.CountResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	count, err := store.Count(ctx, keyRange)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) DeleteRange(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.DeleteResponse, error) {
	kr, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	if kr == nil {
		return nil, status.Error(codes.InvalidArgument, "key range is required for DeleteRange")
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	deleted, err := store.DeleteRange(ctx, *kr)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s *indexedDBServer) IndexGet(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordResponse, error) {
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	rec, err := store.Index(req.GetIndex()).Get(ctx, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordResponseFromRecord(rec)
}

func (s *indexedDBServer) IndexGetKey(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeyResponse, error) {
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	key, err := store.Index(req.GetIndex()).GetKey(ctx, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBServer) IndexGetAll(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordsResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	recs, err := store.Index(req.GetIndex()).GetAll(ctx, keyRange, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordsResponseFromRecords(recs)
}

func (s *indexedDBServer) IndexGetAllKeys(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeysResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	keys, err := store.Index(req.GetIndex()).GetAllKeys(ctx, keyRange, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBServer) IndexCount(ctx context.Context, req *proto.IndexQueryRequest) (*proto.CountResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	count, err := store.Index(req.GetIndex()).Count(ctx, keyRange, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) IndexDelete(ctx context.Context, req *proto.IndexQueryRequest) (*proto.DeleteResponse, error) {
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	deleted, err := store.Index(req.GetIndex()).Delete(ctx, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s *indexedDBServer) Transaction(stream proto.IndexedDB_TransactionServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	begin := first.GetBegin()
	if begin == nil {
		return status.Error(codes.InvalidArgument, "first message must be BeginTransactionRequest")
	}
	stores, err := s.transactionStores(begin.GetStores())
	if err != nil {
		return indexeddbToGRPCErr(err)
	}
	tx, err := metricutil.UnwrapIndexedDB(s.ds).Transaction(
		stream.Context(),
		stores,
		protoTransactionMode(begin.GetMode()),
		coreindexeddb.TransactionOptions{DurabilityHint: protoDurabilityHint(begin.GetDurabilityHint())},
	)
	if err != nil {
		return indexeddbToGRPCErr(err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = tx.Abort(stream.Context())
		}
	}()

	if err := stream.Send(&proto.TransactionServerMessage{
		Msg: &proto.TransactionServerMessage_Begin{Begin: &proto.TransactionBeginResponse{}},
	}); err != nil {
		return err
	}

	for {
		msg, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				finished = true
				_ = tx.Abort(stream.Context())
				return nil
			}
			return recvErr
		}

		switch body := msg.GetMsg().(type) {
		case *proto.TransactionClientMessage_Operation:
			resp, opErr := s.executeTransactionOperation(stream.Context(), tx, body.Operation)
			if opErr != nil {
				finished = true
				abortErr := tx.Abort(stream.Context())
				if err := stream.Send(&proto.TransactionServerMessage{
					Msg: &proto.TransactionServerMessage_Operation{Operation: transactionOperationError(body.Operation.GetRequestId(), opErr)},
				}); err != nil {
					return err
				}
				if err := stream.Send(&proto.TransactionServerMessage{
					Msg: &proto.TransactionServerMessage_Abort{Abort: &proto.TransactionAbortResponse{Error: rpcStatusFromError(abortErr)}},
				}); err != nil {
					return err
				}
				return drainTransactionStream(stream)
			}
			if err := stream.Send(&proto.TransactionServerMessage{
				Msg: &proto.TransactionServerMessage_Operation{Operation: resp},
			}); err != nil {
				return err
			}
		case *proto.TransactionClientMessage_Commit:
			finished = true
			commitErr := tx.Commit(stream.Context())
			return stream.Send(&proto.TransactionServerMessage{
				Msg: &proto.TransactionServerMessage_Commit{Commit: &proto.TransactionCommitResponse{Error: rpcStatusFromError(commitErr)}},
			})
		case *proto.TransactionClientMessage_Abort:
			finished = true
			abortErr := tx.Abort(stream.Context())
			return stream.Send(&proto.TransactionServerMessage{
				Msg: &proto.TransactionServerMessage_Abort{Abort: &proto.TransactionAbortResponse{Error: rpcStatusFromError(abortErr)}},
			})
		default:
			finished = true
			_ = tx.Abort(stream.Context())
			return status.Error(codes.InvalidArgument, "expected transaction operation, commit, or abort")
		}
	}
}

func drainTransactionStream(stream proto.IndexedDB_TransactionServer) error {
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (s *indexedDBServer) transactionStores(stores []string) ([]string, error) {
	if len(stores) == 0 {
		return nil, coreindexeddb.ErrInvalidTransaction
	}
	out := make([]string, len(stores))
	for i, store := range stores {
		if err := s.ensureAllowedStore(store); err != nil {
			return nil, err
		}
		out[i] = s.storeName(store)
	}
	return out, nil
}

func (s *indexedDBServer) transactionObjectStore(tx coreindexeddb.Transaction, name string) (coreindexeddb.TransactionObjectStore, error) {
	if err := s.ensureAllowedStore(name); err != nil {
		return nil, err
	}
	return tx.ObjectStore(s.storeName(name)), nil
}

func (s *indexedDBServer) executeTransactionOperation(ctx context.Context, tx coreindexeddb.Transaction, op *proto.TransactionOperation) (*proto.TransactionOperationResponse, error) {
	if op == nil {
		return nil, status.Error(codes.InvalidArgument, "transaction operation is required")
	}
	resp := &proto.TransactionOperationResponse{RequestId: op.GetRequestId()}
	switch body := op.GetOperation().(type) {
	case *proto.TransactionOperation_Get:
		store, err := s.transactionObjectStore(tx, body.Get.GetStore())
		if err != nil {
			return nil, err
		}
		rec, err := store.Get(ctx, body.Get.GetId())
		if err != nil {
			return nil, err
		}
		pbRec, err := recordResponseFromRecord(rec)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Record{Record: pbRec}
	case *proto.TransactionOperation_GetKey:
		store, err := s.transactionObjectStore(tx, body.GetKey.GetStore())
		if err != nil {
			return nil, err
		}
		key, err := store.GetKey(ctx, body.GetKey.GetId())
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Key{Key: &proto.KeyResponse{Key: key}}
	case *proto.TransactionOperation_Add:
		record, err := recordFromProto(body.Add.GetRecord())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "unmarshal record: %v", err)
		}
		store, err := s.transactionObjectStore(tx, body.Add.GetStore())
		if err != nil {
			return nil, err
		}
		if err := store.Add(ctx, record); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_Put:
		record, err := recordFromProto(body.Put.GetRecord())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "unmarshal record: %v", err)
		}
		store, err := s.transactionObjectStore(tx, body.Put.GetStore())
		if err != nil {
			return nil, err
		}
		if err := store.Put(ctx, record); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_Delete:
		store, err := s.transactionObjectStore(tx, body.Delete.GetStore())
		if err != nil {
			return nil, err
		}
		if err := store.Delete(ctx, body.Delete.GetId()); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_Clear:
		store, err := s.transactionObjectStore(tx, body.Clear.GetStore())
		if err != nil {
			return nil, err
		}
		if err := store.Clear(ctx); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_GetAll:
		keyRange, err := protoToKeyRange(body.GetAll.Range)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
		}
		store, err := s.transactionObjectStore(tx, body.GetAll.GetStore())
		if err != nil {
			return nil, err
		}
		recs, err := store.GetAll(ctx, keyRange)
		if err != nil {
			return nil, err
		}
		pbRecs, err := recordsResponseFromRecords(recs)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Records{Records: pbRecs}
	case *proto.TransactionOperation_GetAllKeys:
		keyRange, err := protoToKeyRange(body.GetAllKeys.Range)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
		}
		store, err := s.transactionObjectStore(tx, body.GetAllKeys.GetStore())
		if err != nil {
			return nil, err
		}
		keys, err := store.GetAllKeys(ctx, keyRange)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Keys{Keys: &proto.KeysResponse{Keys: keys}}
	case *proto.TransactionOperation_Count:
		keyRange, err := protoToKeyRange(body.Count.Range)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
		}
		store, err := s.transactionObjectStore(tx, body.Count.GetStore())
		if err != nil {
			return nil, err
		}
		count, err := store.Count(ctx, keyRange)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Count{Count: &proto.CountResponse{Count: count}}
	case *proto.TransactionOperation_DeleteRange:
		keyRange, err := protoToKeyRange(body.DeleteRange.Range)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
		}
		if keyRange == nil {
			return nil, status.Error(codes.InvalidArgument, "key range is required for DeleteRange")
		}
		store, err := s.transactionObjectStore(tx, body.DeleteRange.GetStore())
		if err != nil {
			return nil, err
		}
		deleted, err := store.DeleteRange(ctx, *keyRange)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Delete{Delete: &proto.DeleteResponse{Deleted: deleted}}
	case *proto.TransactionOperation_IndexGet:
		record, err := s.executeTransactionIndexGet(ctx, tx, body.IndexGet)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Record{Record: record}
	case *proto.TransactionOperation_IndexGetKey:
		key, err := s.executeTransactionIndexGetKey(ctx, tx, body.IndexGetKey)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Key{Key: &proto.KeyResponse{Key: key}}
	case *proto.TransactionOperation_IndexGetAll:
		records, err := s.executeTransactionIndexGetAll(ctx, tx, body.IndexGetAll)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Records{Records: records}
	case *proto.TransactionOperation_IndexGetAllKeys:
		keys, err := s.executeTransactionIndexGetAllKeys(ctx, tx, body.IndexGetAllKeys)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Keys{Keys: &proto.KeysResponse{Keys: keys}}
	case *proto.TransactionOperation_IndexCount:
		count, err := s.executeTransactionIndexCount(ctx, tx, body.IndexCount)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Count{Count: &proto.CountResponse{Count: count}}
	case *proto.TransactionOperation_IndexDelete:
		deleted, err := s.executeTransactionIndexDelete(ctx, tx, body.IndexDelete)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Delete{Delete: &proto.DeleteResponse{Deleted: deleted}}
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown transaction operation")
	}
	return resp, nil
}

func (s *indexedDBServer) transactionIndex(ctx context.Context, tx coreindexeddb.Transaction, req *proto.IndexQueryRequest) (coreindexeddb.TransactionIndex, []any, *coreindexeddb.KeyRange, error) {
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, nil, nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, nil, nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	store, err := s.transactionObjectStore(tx, req.GetStore())
	if err != nil {
		return nil, nil, nil, err
	}
	return store.Index(req.GetIndex()), values, keyRange, nil
}

func (s *indexedDBServer) executeTransactionIndexGet(ctx context.Context, tx coreindexeddb.Transaction, req *proto.IndexQueryRequest) (*proto.RecordResponse, error) {
	idx, values, _, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	rec, err := idx.Get(ctx, values...)
	if err != nil {
		return nil, err
	}
	return recordResponseFromRecord(rec)
}

func (s *indexedDBServer) executeTransactionIndexGetKey(ctx context.Context, tx coreindexeddb.Transaction, req *proto.IndexQueryRequest) (string, error) {
	idx, values, _, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return "", err
	}
	return idx.GetKey(ctx, values...)
}

func (s *indexedDBServer) executeTransactionIndexGetAll(ctx context.Context, tx coreindexeddb.Transaction, req *proto.IndexQueryRequest) (*proto.RecordsResponse, error) {
	idx, values, keyRange, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	recs, err := idx.GetAll(ctx, keyRange, values...)
	if err != nil {
		return nil, err
	}
	return recordsResponseFromRecords(recs)
}

func (s *indexedDBServer) executeTransactionIndexGetAllKeys(ctx context.Context, tx coreindexeddb.Transaction, req *proto.IndexQueryRequest) ([]string, error) {
	idx, values, keyRange, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	return idx.GetAllKeys(ctx, keyRange, values...)
}

func (s *indexedDBServer) executeTransactionIndexCount(ctx context.Context, tx coreindexeddb.Transaction, req *proto.IndexQueryRequest) (int64, error) {
	idx, values, keyRange, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return 0, err
	}
	return idx.Count(ctx, keyRange, values...)
}

func (s *indexedDBServer) executeTransactionIndexDelete(ctx context.Context, tx coreindexeddb.Transaction, req *proto.IndexQueryRequest) (int64, error) {
	idx, values, _, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return 0, err
	}
	return idx.Delete(ctx, values...)
}

func protoTransactionMode(mode proto.TransactionMode) coreindexeddb.TransactionMode {
	if mode == proto.TransactionMode_TRANSACTION_READWRITE {
		return coreindexeddb.TransactionReadwrite
	}
	return coreindexeddb.TransactionReadonly
}

func protoDurabilityHint(hint proto.TransactionDurabilityHint) coreindexeddb.TransactionDurabilityHint {
	switch hint {
	case proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_STRICT:
		return coreindexeddb.TransactionDurabilityStrict
	case proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_RELAXED:
		return coreindexeddb.TransactionDurabilityRelaxed
	default:
		return coreindexeddb.TransactionDurabilityDefault
	}
}

func transactionOperationError(requestID uint64, err error) *proto.TransactionOperationResponse {
	return &proto.TransactionOperationResponse{
		RequestId: requestID,
		Error:     rpcStatusFromError(err),
	}
}

func rpcStatusFromError(err error) *rpcstatus.Status {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok {
		return &rpcstatus.Status{Code: int32(st.Code()), Message: st.Message()}
	}
	grpcErr := indexeddbToGRPCErr(err)
	st, ok := status.FromError(grpcErr)
	if !ok {
		return &rpcstatus.Status{Code: int32(codes.Internal), Message: err.Error()}
	}
	return &rpcstatus.Status{Code: int32(st.Code()), Message: st.Message()}
}

func protoCursorDirection(d proto.CursorDirection) coreindexeddb.CursorDirection {
	switch d {
	case proto.CursorDirection_CURSOR_NEXT_UNIQUE:
		return coreindexeddb.CursorNextUnique
	case proto.CursorDirection_CURSOR_PREV:
		return coreindexeddb.CursorPrev
	case proto.CursorDirection_CURSOR_PREV_UNIQUE:
		return coreindexeddb.CursorPrevUnique
	default:
		return coreindexeddb.CursorNext
	}
}

func (s *indexedDBServer) OpenCursor(stream proto.IndexedDB_OpenCursorServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	openReq := first.GetOpen()
	if openReq == nil {
		return status.Error(codes.InvalidArgument, "first message must be OpenCursorRequest")
	}

	keyRange, err := protoToKeyRange(openReq.Range)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	dir := protoCursorDirection(openReq.GetDirection())
	ctx := stream.Context()

	var cursor coreindexeddb.Cursor
	store, err := s.objectStore(openReq.GetStore())
	if err != nil {
		return indexeddbToGRPCErr(err)
	}

	if openReq.GetIndex() != "" {
		values, vErr := protoValuesToAny(openReq.GetValues())
		if vErr != nil {
			return status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", vErr)
		}
		idx := store.Index(openReq.GetIndex())
		if openReq.GetKeysOnly() {
			cursor, err = idx.OpenKeyCursor(ctx, keyRange, dir, values...)
		} else {
			cursor, err = idx.OpenCursor(ctx, keyRange, dir, values...)
		}
	} else {
		if openReq.GetKeysOnly() {
			cursor, err = store.OpenKeyCursor(ctx, keyRange, dir)
		} else {
			cursor, err = store.OpenCursor(ctx, keyRange, dir)
		}
	}
	if err != nil {
		return indexeddbToGRPCErr(err)
	}
	defer func() { _ = cursor.Close() }()

	// Send an open ack so clients can detect failures synchronously.
	if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Done{}}); sErr != nil {
		return sErr
	}

	for {
		msg, recvErr := stream.Recv()
		if recvErr != nil {
			return recvErr
		}
		cmd := msg.GetCommand()
		if cmd == nil {
			return status.Error(codes.InvalidArgument, "expected CursorCommand after open")
		}

		switch v := cmd.GetCommand().(type) {
		case *proto.CursorCommand_Next:
			if !cursor.Continue() {
				if cErr := cursor.Err(); cErr != nil {
					return indexeddbToGRPCErr(cErr)
				}
				if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Done{Done: true}}); sErr != nil {
					return sErr
				}
				continue
			}
			entry, eErr := cursorEntryToProto(cursor, openReq.GetKeysOnly())
			if eErr != nil {
				return eErr
			}
			if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Entry{Entry: entry}}); sErr != nil {
				return sErr
			}

		case *proto.CursorCommand_ContinueToKey:
			target := v.ContinueToKey.GetKey()
			if len(target) == 0 {
				return status.Error(codes.InvalidArgument, "continue key is required")
			}
			parts, kErr := keyValuesToAny(target)
			if kErr != nil {
				return status.Errorf(codes.InvalidArgument, "unmarshal continue key: %v", kErr)
			}
			var key any
			switch {
			case openReq.GetIndex() != "":
				key = parts
			case len(parts) == 1:
				key = parts[0]
			default:
				key = parts
			}
			if !cursor.ContinueToKey(key) {
				if cErr := cursor.Err(); cErr != nil {
					return indexeddbToGRPCErr(cErr)
				}
				if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Done{Done: true}}); sErr != nil {
					return sErr
				}
				continue
			}
			entry, eErr := cursorEntryToProto(cursor, openReq.GetKeysOnly())
			if eErr != nil {
				return eErr
			}
			if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Entry{Entry: entry}}); sErr != nil {
				return sErr
			}

		case *proto.CursorCommand_Advance:
			if v.Advance <= 0 {
				return status.Error(codes.InvalidArgument, "advance count must be positive")
			}
			if !cursor.Advance(int(v.Advance)) {
				if cErr := cursor.Err(); cErr != nil {
					return indexeddbToGRPCErr(cErr)
				}
				if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Done{Done: true}}); sErr != nil {
					return sErr
				}
				continue
			}
			entry, eErr := cursorEntryToProto(cursor, openReq.GetKeysOnly())
			if eErr != nil {
				return eErr
			}
			if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Entry{Entry: entry}}); sErr != nil {
				return sErr
			}

		case *proto.CursorCommand_Delete:
			if dErr := cursor.Delete(); dErr != nil {
				return indexeddbToGRPCErr(dErr)
			}
			if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Done{}}); sErr != nil {
				return sErr
			}

		case *proto.CursorCommand_Update:
			rec, rErr := recordFromProto(v.Update)
			if rErr != nil {
				return status.Errorf(codes.InvalidArgument, "unmarshal update record: %v", rErr)
			}
			if uErr := cursor.Update(rec); uErr != nil {
				return indexeddbToGRPCErr(uErr)
			}
			entry, eErr := cursorEntryToProto(cursor, openReq.GetKeysOnly())
			if eErr != nil {
				return eErr
			}
			if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Entry{Entry: entry}}); sErr != nil {
				return sErr
			}

		case *proto.CursorCommand_Close:
			return nil

		default:
			return status.Error(codes.InvalidArgument, "unknown cursor command")
		}
	}
}

func cursorEntryToProto(c coreindexeddb.Cursor, keysOnly bool) (*proto.CursorEntry, error) {
	entry := &proto.CursorEntry{PrimaryKey: c.PrimaryKey()}
	key := c.Key()
	if key != nil {
		if parts, ok := key.([]any); ok {
			kvs := make([]*proto.KeyValue, len(parts))
			for i, p := range parts {
				kv, err := anyToKeyValue(p)
				if err != nil {
					return nil, fmt.Errorf("marshal cursor key[%d]: %w", i, err)
				}
				kvs[i] = kv
			}
			entry.Key = kvs
		} else {
			kv, err := anyToKeyValue(key)
			if err != nil {
				return nil, fmt.Errorf("marshal cursor key: %w", err)
			}
			entry.Key = []*proto.KeyValue{kv}
		}
	}
	if !keysOnly {
		rec, err := c.Value()
		if err != nil {
			return nil, fmt.Errorf("cursor value: %w", err)
		}
		pbRec, err := recordToProto(rec)
		if err != nil {
			return nil, fmt.Errorf("marshal cursor record: %w", err)
		}
		entry.Record = pbRec
	}
	return entry, nil
}

func recordResponseFromRecord(rec coreindexeddb.Record) (*proto.RecordResponse, error) {
	pbRecord, err := recordToProto(rec)
	if err != nil {
		return nil, fmt.Errorf("marshal record: %w", err)
	}
	return &proto.RecordResponse{Record: pbRecord}, nil
}

func recordsResponseFromRecords(recs []coreindexeddb.Record) (*proto.RecordsResponse, error) {
	pbRecords, err := recordsToProto(recs)
	if err != nil {
		return nil, err
	}
	return &proto.RecordsResponse{Records: pbRecords}, nil
}

func protoToSchema(ps *proto.ObjectStoreSchema) coreindexeddb.ObjectStoreSchema {
	if ps == nil {
		return coreindexeddb.ObjectStoreSchema{}
	}
	schema := coreindexeddb.ObjectStoreSchema{
		Indexes: make([]coreindexeddb.IndexSchema, len(ps.GetIndexes())),
		Columns: make([]coreindexeddb.ColumnDef, len(ps.GetColumns())),
	}
	for i, idx := range ps.GetIndexes() {
		schema.Indexes[i] = coreindexeddb.IndexSchema{
			Name: idx.GetName(), KeyPath: idx.GetKeyPath(), Unique: idx.GetUnique(),
		}
	}
	for i, col := range ps.GetColumns() {
		schema.Columns[i] = coreindexeddb.ColumnDef{
			Name: col.GetName(), Type: coreindexeddb.ColumnType(col.GetType()),
			PrimaryKey: col.GetPrimaryKey(), NotNull: col.GetNotNull(), Unique: col.GetUnique(),
		}
	}
	return schema
}

func protoToKeyRange(kr *proto.KeyRange) (*coreindexeddb.KeyRange, error) {
	if kr == nil {
		return nil, nil
	}
	r := &coreindexeddb.KeyRange{
		LowerOpen: kr.GetLowerOpen(),
		UpperOpen: kr.GetUpperOpen(),
	}
	if kr.GetLower() != nil {
		value, err := anyFromTypedValue(kr.GetLower())
		if err != nil {
			return nil, fmt.Errorf("key range lower: %w", err)
		}
		r.Lower = value
	}
	if kr.GetUpper() != nil {
		value, err := anyFromTypedValue(kr.GetUpper())
		if err != nil {
			return nil, fmt.Errorf("key range upper: %w", err)
		}
		r.Upper = value
	}
	return r, nil
}

func protoValuesToAny(vals []*proto.TypedValue) ([]any, error) {
	return anyFromTypedValues(vals)
}

func indexeddbToGRPCErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, coreindexeddb.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, coreindexeddb.ErrAlreadyExists) {
		return status.Error(codes.AlreadyExists, err.Error())
	}
	if errors.Is(err, coreindexeddb.ErrInvalidTransaction) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if errors.Is(err, coreindexeddb.ErrReadOnly) || errors.Is(err, coreindexeddb.ErrTransactionDone) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}
