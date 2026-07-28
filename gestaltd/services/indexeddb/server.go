package indexeddb

//go:generate go run ../../tools/routinggen -grpc ../../rpc/protov1/v1/indexeddb_grpc.pb.go -service IndexedDBServer -receiver routingIndexedDBServer -binding indexeddb -package indexeddb -server-type proto.IndexedDBServer -output routing_indexeddb_gen.go

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type indexedDBServer struct {
	proto.UnimplementedIndexedDBServer
	ds           coreindexeddb.IndexedDB
	db           string
	plugin       string
	allowed      map[string]struct{}
	storeNames   StoreNameResolver
	storeTracker NamespaceStoreTracker
}

type ServerOptions struct {
	AllowedStores []string
	StoreNames    StoreNameResolver
	StoreTracker  NamespaceStoreTracker
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
		ds:           ds,
		db:           metricutil.IndexedDBName(ds),
		plugin:       pluginName,
		allowed:      allowed,
		storeNames:   opts.StoreNames,
		storeTracker: opts.StoreTracker,
	}
}

// StoreNameResolver maps a logical object-store name to the physical name used
// by the underlying provider. When namespace isolation is active the resolver
// also returns a ResolvedStoreScope describing the mapping.
type StoreNameResolver interface {
	ResolveStoreName(ctx context.Context, logicalName string) (physicalName string, scope *ResolvedStoreScope, err error)
}

// ResolvedStoreScope carries the namespace identity behind a store-name mapping.
type ResolvedStoreScope struct {
	NamespaceID  string
	LogicalName  string
	PhysicalName string
}

// NamespaceStoreTracker records the intent to create or delete a physical
// object store so that asynchronous cleanup can find every store that may
// have been created.
type NamespaceStoreTracker interface {
	TrackStore(ctx context.Context, scope ResolvedStoreScope) error
	MarkStoreDeleted(ctx context.Context, scope ResolvedStoreScope) error
}

type routingIndexedDBServer struct {
	proto.UnimplementedIndexedDBServer
	servers        map[string]proto.IndexedDBServer
	defaultBinding string
}

func NewRoutingServer(bindings map[string]coreindexeddb.IndexedDB, defaultBinding string, pluginName string, opts ServerOptions) proto.IndexedDBServer {
	servers := make(map[string]proto.IndexedDBServer, len(bindings))
	for name, ds := range bindings {
		name = strings.TrimSpace(name)
		if name == "" || ds == nil {
			continue
		}
		servers[name] = NewServer(ds, pluginName, opts)
	}
	defaultBinding = strings.TrimSpace(defaultBinding)
	if defaultBinding == "" && len(servers) == 1 {
		for name := range servers {
			defaultBinding = name
		}
	}
	return &routingIndexedDBServer{servers: servers, defaultBinding: defaultBinding}
}

func (s *routingIndexedDBServer) server(ctx context.Context) (proto.IndexedDBServer, error) {
	return runtimehost.ResolveBinding(ctx, "indexeddb", s.defaultBinding, s.servers)
}

func (s *routingIndexedDBServer) OpenCursor(stream proto.IndexedDB_OpenCursorServer) error {
	server, err := s.server(stream.Context())
	if err != nil {
		return err
	}
	return server.OpenCursor(stream)
}

func (s *routingIndexedDBServer) Transaction(stream proto.IndexedDB_TransactionServer) error {
	server, err := s.server(stream.Context())
	if err != nil {
		return err
	}
	return server.Transaction(stream)
}

func (s *indexedDBServer) ensureAllowedStore(name string) error {
	if len(s.allowed) == 0 {
		return nil
	}
	if _, ok := s.allowed[name]; ok {
		return nil
	}
	return idb.ErrNotFound
}

func (s *indexedDBServer) resolveStoreName(ctx context.Context, logical string) (string, *ResolvedStoreScope, error) {
	if err := s.ensureAllowedStore(logical); err != nil {
		return "", nil, err
	}
	if s.storeNames == nil {
		return logical, nil, nil
	}
	physical, scope, err := s.storeNames.ResolveStoreName(ctx, logical)
	if err != nil {
		return "", nil, err
	}
	return physical, scope, nil
}

func (s *indexedDBServer) resolvePhysicalStoreName(ctx context.Context, logical string) (string, error) {
	physical, _, err := s.resolveStoreName(ctx, logical)
	return physical, err
}

func (s *indexedDBServer) objectStore(ctx context.Context, name string) (idb.ObjectStore, error) {
	physicalName, _, err := s.resolveStoreName(ctx, name)
	if err != nil {
		return nil, err
	}
	return metricutil.InstrumentObjectStore(
		metricutil.UnwrapIndexedDB(s.ds).ObjectStore(physicalName),
		metricutil.IndexedDBMetricLabels{
			DB:           s.db,
			ProviderName: s.plugin,
			ObjectStore:  name,
		},
	), nil
}

func (s *indexedDBServer) CreateObjectStore(ctx context.Context, req *proto.CreateObjectStoreRequest) (*emptypb.Empty, error) {
	physical, scope, err := s.resolveStoreName(ctx, req.GetName())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if scope != nil && s.storeTracker != nil {
		if err := s.storeTracker.TrackStore(ctx, ResolvedStoreScope{
			NamespaceID:  scope.NamespaceID,
			LogicalName:  req.GetName(),
			PhysicalName: physical,
		}); err != nil {
			return nil, status.Error(codes.Unavailable, "indexeddb namespace tracking unavailable")
		}
	}
	schema := protoToSchema(req.GetSchema())
	if _, err := s.ds.CreateObjectStore(ctx, physical, schema); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) DeleteObjectStore(ctx context.Context, req *proto.DeleteObjectStoreRequest) (*emptypb.Empty, error) {
	physical, scope, err := s.resolveStoreName(ctx, req.GetName())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := s.ds.DeleteObjectStore(ctx, physical); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if scope != nil && s.storeTracker != nil {
		if err := s.storeTracker.MarkStoreDeleted(ctx, *scope); err != nil {
			return nil, status.Error(codes.Unavailable, "indexeddb namespace tracking unavailable")
		}
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) CreateIndex(ctx context.Context, req *proto.CreateIndexRequest) (*emptypb.Empty, error) {
	physicalName, _, err := s.resolveStoreName(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	manager, ok := metricutil.UnwrapIndexedDB(s.ds).(idb.IndexManager)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "indexeddb: index management not supported")
	}
	index := idb.IndexDefinition{Name: req.GetName(), KeyPath: req.GetKeyPath(), Unique: req.GetUnique()}
	if err := manager.CreateIndex(ctx, physicalName, index); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) DeleteIndex(ctx context.Context, req *proto.DeleteIndexRequest) (*emptypb.Empty, error) {
	physicalName, _, err := s.resolveStoreName(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	manager, ok := metricutil.UnwrapIndexedDB(s.ds).(idb.IndexManager)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "indexeddb: index management not supported")
	}
	if err := manager.DeleteIndex(ctx, physicalName, req.GetName()); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Get(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	store, err := s.objectStore(ctx, req.GetStore())
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
	store, err := s.objectStore(ctx, req.GetStore())
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
	store, err := s.objectStore(ctx, req.GetStore())
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
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Put(ctx, record); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Delete(ctx context.Context, req *proto.ObjectStoreRequest) (*emptypb.Empty, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Delete(ctx, req.GetId()); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Clear(ctx context.Context, req *proto.ObjectStoreNameRequest) (*emptypb.Empty, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Clear(ctx); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) GetAll(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.RecordsResponse, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	var recs []idb.Record
	if req.Count != nil && *req.Count > 0 {
		recs, err = store.GetAll(ctx, req.GetQuery(), *req.Count)
	} else {
		recs, err = store.GetAll(ctx, req.GetQuery())
	}
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordsResponseFromRecords(recs)
}

func (s *indexedDBServer) GetAllKeys(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.KeysResponse, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	var keys []string
	if req.Count != nil && *req.Count > 0 {
		keys, err = store.GetAllKeys(ctx, req.GetQuery(), *req.Count)
	} else {
		keys, err = store.GetAllKeys(ctx, req.GetQuery())
	}
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBServer) Count(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.CountResponse, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	count, err := store.Count(ctx, req.GetQuery())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) DeleteRange(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.DeleteResponse, error) {
	if req.GetQuery() == nil {
		return nil, status.Error(codes.InvalidArgument, "query is required for DeleteRange")
	}
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	deleted, err := store.DeleteRange(ctx, req.GetQuery())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s *indexedDBServer) IndexGet(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordResponse, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	rec, err := store.Index(req.GetIndex()).Get(ctx, req.GetQuery())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordResponseFromRecord(rec)
}

func (s *indexedDBServer) IndexGetKey(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeyResponse, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	key, err := store.Index(req.GetIndex()).GetKey(ctx, req.GetQuery())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBServer) IndexGetAll(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordsResponse, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	var recs []idb.Record
	if req.Count != nil && *req.Count > 0 {
		recs, err = store.Index(req.GetIndex()).GetAll(ctx, req.GetQuery(), *req.Count)
	} else {
		recs, err = store.Index(req.GetIndex()).GetAll(ctx, req.GetQuery())
	}
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordsResponseFromRecords(recs)
}

func (s *indexedDBServer) IndexGetAllKeys(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeysResponse, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	var keys []string
	if req.Count != nil && *req.Count > 0 {
		keys, err = store.Index(req.GetIndex()).GetAllKeys(ctx, req.GetQuery(), *req.Count)
	} else {
		keys, err = store.Index(req.GetIndex()).GetAllKeys(ctx, req.GetQuery())
	}
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBServer) IndexCount(ctx context.Context, req *proto.IndexQueryRequest) (*proto.CountResponse, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	count, err := store.Index(req.GetIndex()).Count(ctx, req.GetQuery())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) IndexDelete(ctx context.Context, req *proto.IndexQueryRequest) (*proto.DeleteResponse, error) {
	store, err := s.objectStore(ctx, req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	deleted, err := store.Index(req.GetIndex()).Delete(ctx, req.GetQuery())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s *indexedDBServer) Transaction(stream proto.IndexedDB_TransactionServer) error {
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	begin := first.GetBegin()
	if begin == nil {
		return status.Error(codes.InvalidArgument, "first message must be BeginTransactionRequest")
	}
	stores, err := s.transactionStores(ctx, begin.GetStores())
	if err != nil {
		return indexeddbToGRPCErr(err)
	}
	tx, err := metricutil.UnwrapIndexedDB(s.ds).Transaction(
		ctx,
		stores,
		protoTransactionMode(begin.GetMode()),
		idb.TransactionOptions{DurabilityHint: protoDurabilityHint(begin.GetDurabilityHint())},
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

func (s *indexedDBServer) transactionStores(ctx context.Context, stores []string) ([]string, error) {
	if len(stores) == 0 {
		return nil, idb.ErrInvalidTransaction
	}
	out := make([]string, len(stores))
	for i, store := range stores {
		physical, _, err := s.resolveStoreName(ctx, store)
		if err != nil {
			return nil, err
		}
		out[i] = physical
	}
	return out, nil
}

func (s *indexedDBServer) transactionObjectStore(ctx context.Context, tx idb.Transaction, name string) (idb.TransactionObjectStore, error) {
	physical, _, err := s.resolveStoreName(ctx, name)
	if err != nil {
		return nil, err
	}
	return tx.ObjectStore(physical), nil
}

func (s *indexedDBServer) executeTransactionOperation(ctx context.Context, tx idb.Transaction, op *proto.TransactionOperation) (*proto.TransactionOperationResponse, error) {
	if op == nil {
		return nil, status.Error(codes.InvalidArgument, "transaction operation is required")
	}
	resp := &proto.TransactionOperationResponse{RequestId: op.GetRequestId()}
	switch body := op.GetOperation().(type) {
	case *proto.TransactionOperation_Get:
		store, err := s.transactionObjectStore(ctx, tx, body.Get.GetStore())
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
		store, err := s.transactionObjectStore(ctx, tx, body.GetKey.GetStore())
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
		store, err := s.transactionObjectStore(ctx, tx, body.Add.GetStore())
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
		store, err := s.transactionObjectStore(ctx, tx, body.Put.GetStore())
		if err != nil {
			return nil, err
		}
		if err := store.Put(ctx, record); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_Delete:
		store, err := s.transactionObjectStore(ctx, tx, body.Delete.GetStore())
		if err != nil {
			return nil, err
		}
		if err := store.Delete(ctx, body.Delete.GetId()); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_Clear:
		store, err := s.transactionObjectStore(ctx, tx, body.Clear.GetStore())
		if err != nil {
			return nil, err
		}
		if err := store.Clear(ctx); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_GetAll:
		store, err := s.transactionObjectStore(ctx, tx, body.GetAll.GetStore())
		if err != nil {
			return nil, err
		}
		var recs []idb.Record
		if body.GetAll.Count != nil && *body.GetAll.Count > 0 {
			recs, err = store.GetAll(ctx, body.GetAll.GetQuery(), *body.GetAll.Count)
		} else {
			recs, err = store.GetAll(ctx, body.GetAll.GetQuery())
		}
		if err != nil {
			return nil, err
		}
		pbRecs, err := recordsResponseFromRecords(recs)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Records{Records: pbRecs}
	case *proto.TransactionOperation_GetAllKeys:
		store, err := s.transactionObjectStore(ctx, tx, body.GetAllKeys.GetStore())
		if err != nil {
			return nil, err
		}
		var keys []string
		if body.GetAllKeys.Count != nil && *body.GetAllKeys.Count > 0 {
			keys, err = store.GetAllKeys(ctx, body.GetAllKeys.GetQuery(), *body.GetAllKeys.Count)
		} else {
			keys, err = store.GetAllKeys(ctx, body.GetAllKeys.GetQuery())
		}
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Keys{Keys: &proto.KeysResponse{Keys: keys}}
	case *proto.TransactionOperation_Count:
		store, err := s.transactionObjectStore(ctx, tx, body.Count.GetStore())
		if err != nil {
			return nil, err
		}
		count, err := store.Count(ctx, body.Count.GetQuery())
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Count{Count: &proto.CountResponse{Count: count}}
	case *proto.TransactionOperation_DeleteRange:
		if body.DeleteRange.GetQuery() == nil {
			return nil, status.Error(codes.InvalidArgument, "query is required for DeleteRange")
		}
		store, err := s.transactionObjectStore(ctx, tx, body.DeleteRange.GetStore())
		if err != nil {
			return nil, err
		}
		deleted, err := store.DeleteRange(ctx, body.DeleteRange.GetQuery())
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

func (s *indexedDBServer) transactionIndex(ctx context.Context, tx idb.Transaction, req *proto.IndexQueryRequest) (idb.TransactionIndex, error) {
	store, err := s.transactionObjectStore(ctx, tx, req.GetStore())
	if err != nil {
		return nil, err
	}
	return store.Index(req.GetIndex()), nil
}

func (s *indexedDBServer) executeTransactionIndexGet(ctx context.Context, tx idb.Transaction, req *proto.IndexQueryRequest) (*proto.RecordResponse, error) {
	idx, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	rec, err := idx.Get(ctx, req.GetQuery())
	if err != nil {
		return nil, err
	}
	return recordResponseFromRecord(rec)
}

func (s *indexedDBServer) executeTransactionIndexGetKey(ctx context.Context, tx idb.Transaction, req *proto.IndexQueryRequest) (string, error) {
	idx, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return "", err
	}
	return idx.GetKey(ctx, req.GetQuery())
}

func (s *indexedDBServer) executeTransactionIndexGetAll(ctx context.Context, tx idb.Transaction, req *proto.IndexQueryRequest) (*proto.RecordsResponse, error) {
	idx, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	var recs []idb.Record
	if req.Count != nil && *req.Count > 0 {
		recs, err = idx.GetAll(ctx, req.GetQuery(), *req.Count)
	} else {
		recs, err = idx.GetAll(ctx, req.GetQuery())
	}
	if err != nil {
		return nil, err
	}
	return recordsResponseFromRecords(recs)
}

func (s *indexedDBServer) executeTransactionIndexGetAllKeys(ctx context.Context, tx idb.Transaction, req *proto.IndexQueryRequest) ([]string, error) {
	idx, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	if req.Count != nil && *req.Count > 0 {
		return idx.GetAllKeys(ctx, req.GetQuery(), *req.Count)
	}
	return idx.GetAllKeys(ctx, req.GetQuery())
}

func (s *indexedDBServer) executeTransactionIndexCount(ctx context.Context, tx idb.Transaction, req *proto.IndexQueryRequest) (int64, error) {
	idx, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return 0, err
	}
	return idx.Count(ctx, req.GetQuery())
}

func (s *indexedDBServer) executeTransactionIndexDelete(ctx context.Context, tx idb.Transaction, req *proto.IndexQueryRequest) (int64, error) {
	idx, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return 0, err
	}
	return idx.Delete(ctx, req.GetQuery())
}

func protoTransactionMode(mode proto.TransactionMode) idb.TransactionMode {
	if mode == proto.TransactionMode_TRANSACTION_READWRITE {
		return idb.TransactionReadwrite
	}
	return idb.TransactionReadonly
}

func protoDurabilityHint(hint proto.TransactionDurabilityHint) idb.TransactionDurabilityHint {
	switch hint {
	case proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_STRICT:
		return idb.TransactionDurabilityStrict
	case proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_RELAXED:
		return idb.TransactionDurabilityRelaxed
	default:
		return idb.TransactionDurabilityDefault
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

func protoCursorDirection(d proto.CursorDirection) idb.CursorDirection {
	switch d {
	case proto.CursorDirection_CURSOR_NEXT_UNIQUE:
		return idb.CursorNextUnique
	case proto.CursorDirection_CURSOR_PREV:
		return idb.CursorPrev
	case proto.CursorDirection_CURSOR_PREV_UNIQUE:
		return idb.CursorPrevUnique
	default:
		return idb.CursorNext
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

	dir := protoCursorDirection(openReq.GetDirection())
	ctx := stream.Context()

	var cursor idb.Cursor
	store, err := s.objectStore(ctx, openReq.GetStore())
	if err != nil {
		return indexeddbToGRPCErr(err)
	}

	if openReq.GetIndex() != "" {
		idx := store.Index(openReq.GetIndex())
		if openReq.GetKeysOnly() {
			cursor, err = idx.OpenKeyCursor(ctx, openReq.GetQuery(), dir)
		} else {
			cursor, err = idx.OpenCursor(ctx, openReq.GetQuery(), dir)
		}
	} else {
		if openReq.GetKeysOnly() {
			cursor, err = store.OpenKeyCursor(ctx, openReq.GetQuery(), dir)
		} else {
			cursor, err = store.OpenCursor(ctx, openReq.GetQuery(), dir)
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
			if errors.Is(recvErr, io.EOF) {
				return nil
			}
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
			if target == nil {
				return status.Error(codes.InvalidArgument, "continue key is required")
			}
			targetKey, dErr := indexeddbcodec.KeyValueToAny(target)
			if dErr != nil {
				return status.Error(codes.InvalidArgument, dErr.Error())
			}
			if !cursor.ContinueToKey(targetKey) {
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

func cursorEntryToProto(c idb.Cursor, keysOnly bool) (*proto.CursorEntry, error) {
	entry := &proto.CursorEntry{PrimaryKey: c.PrimaryKey()}
	if key := c.Key(); key != nil {
		kv, err := anyToKeyValue(key)
		if err != nil {
			return nil, fmt.Errorf("marshal cursor key: %w", err)
		}
		entry.Key = kv
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

func recordResponseFromRecord(rec idb.Record) (*proto.RecordResponse, error) {
	pbRecord, err := recordToProto(rec)
	if err != nil {
		return nil, fmt.Errorf("marshal record: %w", err)
	}
	return &proto.RecordResponse{Record: pbRecord}, nil
}

func recordsResponseFromRecords(recs []idb.Record) (*proto.RecordsResponse, error) {
	pbRecords, err := recordsToProto(recs)
	if err != nil {
		return nil, err
	}
	return &proto.RecordsResponse{Records: pbRecords}, nil
}

func protoToSchema(ps *proto.ObjectStoreSchema) idb.ObjectStoreOptions {
	if ps == nil {
		return idb.ObjectStoreOptions{}
	}
	schema := idb.ObjectStoreOptions{
		Indexes: make([]idb.IndexSchema, len(ps.GetIndexes())),
		Columns: make([]idb.ColumnDef, len(ps.GetColumns())),
	}
	for i, idx := range ps.GetIndexes() {
		schema.Indexes[i] = idb.IndexSchema{
			Name: idx.GetName(), KeyPath: idx.GetKeyPath(), Unique: idx.GetUnique(),
		}
	}
	for i, col := range ps.GetColumns() {
		schema.Columns[i] = idb.ColumnDef{
			Name: col.GetName(), Type: idb.ColumnType(col.GetType()),
			PrimaryKey: col.GetPrimaryKey(), NotNull: col.GetNotNull(), Unique: col.GetUnique(),
		}
	}
	return schema
}

func indexeddbToGRPCErr(err error) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok {
		return status.Error(st.Code(), st.Message())
	}
	if errors.Is(err, idb.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, idb.ErrAlreadyExists) {
		return status.Error(codes.AlreadyExists, err.Error())
	}
	if errors.Is(err, idb.ErrInvalidTransaction) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if errors.Is(err, idb.ErrReadOnly) || errors.Is(err, idb.ErrTransactionDone) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}
