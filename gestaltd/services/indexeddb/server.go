package indexeddb

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"

	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type indexedDBServer struct {
	proto.UnimplementedIndexedDBServer
	ds               coreindexeddb.IndexedDB
	factory          coreindexeddb.Factory
	db               string
	plugin           string
	allowed          map[string]struct{}
	allowedDatabases map[string]struct{}
	registry         *ConnectionRegistry
}

// ConnectionRegistry shares live OpenDatabase connection handles across
// IndexedDB server instances registered for the same host service.
type ConnectionRegistry struct {
	mu          sync.Mutex
	connections map[string]*databaseConnection
}

// NewConnectionRegistry creates an empty connection registry.
func NewConnectionRegistry() *ConnectionRegistry {
	return &ConnectionRegistry{connections: make(map[string]*databaseConnection)}
}

type databaseConnection struct {
	db      coreindexeddb.Database
	mu      sync.Mutex
	cond    *sync.Cond
	active  int
	closing bool
}

func newDatabaseConnection(db coreindexeddb.Database) *databaseConnection {
	conn := &databaseConnection{db: db}
	conn.cond = sync.NewCond(&conn.mu)
	return conn
}

func (c *databaseConnection) acquire() (coreindexeddb.Database, func(), error) {
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return nil, nil, status.Error(codes.FailedPrecondition, "indexeddb database connection is closing")
	}
	c.active++
	c.mu.Unlock()
	return c.db, c.release, nil
}

func (c *databaseConnection) release() {
	c.mu.Lock()
	c.active--
	if c.active == 0 {
		c.cond.Broadcast()
	}
	c.mu.Unlock()
}

func (c *databaseConnection) close() {
	c.mu.Lock()
	c.closing = true
	for c.active > 0 {
		c.cond.Wait()
	}
	c.mu.Unlock()
	_ = c.db.Close()
}

type ServerOptions struct {
	AllowedStores      []string
	AllowedDatabases   []string
	ConnectionRegistry *ConnectionRegistry
}

func NewServer(ds coreindexeddb.IndexedDB, pluginName string, opts ServerOptions) proto.IndexedDBServer {
	allowed := make(map[string]struct{}, len(opts.AllowedStores))
	for _, store := range opts.AllowedStores {
		allowed[store] = struct{}{}
	}
	if len(allowed) == 0 {
		allowed = nil
	}
	allowedDatabases := make(map[string]struct{}, len(opts.AllowedDatabases))
	for _, database := range opts.AllowedDatabases {
		allowedDatabases[database] = struct{}{}
	}
	if len(allowedDatabases) == 0 {
		allowedDatabases = nil
	}
	var factory coreindexeddb.Factory
	if candidate, ok := metricutil.UnwrapIndexedDB(ds).(coreindexeddb.Factory); ok {
		factory = candidate
	}
	registry := opts.ConnectionRegistry
	if registry == nil {
		registry = NewConnectionRegistry()
	}
	return &indexedDBServer{
		ds:               ds,
		factory:          factory,
		db:               metricutil.IndexedDBName(ds),
		plugin:           pluginName,
		allowed:          allowed,
		allowedDatabases: allowedDatabases,
		registry:         registry,
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

func (s *indexedDBServer) filterStoreNames(names []string) []string {
	if len(s.allowed) == 0 {
		return append([]string(nil), names...)
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := s.allowed[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

func (s *indexedDBServer) ensureAllowedDatabase(name string) error {
	if len(s.allowedDatabases) == 0 {
		return nil
	}
	if _, ok := s.allowedDatabases[name]; ok {
		return nil
	}
	return coreindexeddb.ErrNotFound
}

func (s *indexedDBServer) registerDatabase(db coreindexeddb.Database) ([]byte, error) {
	for range 8 {
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return nil, err
		}
		key := string(id)
		s.registry.mu.Lock()
		if _, exists := s.registry.connections[key]; !exists {
			s.registry.connections[key] = newDatabaseConnection(db)
			s.registry.mu.Unlock()
			return id, nil
		}
		s.registry.mu.Unlock()
	}
	return nil, status.Error(codes.Internal, "could not allocate database connection id")
}

func (s *indexedDBServer) databaseForConnection(connectionID []byte) (coreindexeddb.Database, func(), error) {
	if len(connectionID) == 0 {
		return nil, func() {}, nil
	}
	s.registry.mu.Lock()
	conn := s.registry.connections[string(connectionID)]
	s.registry.mu.Unlock()
	if conn == nil {
		return nil, nil, coreindexeddb.ErrNotFound
	}
	return conn.acquire()
}

func (s *indexedDBServer) closeConnection(connectionID []byte) {
	if len(connectionID) == 0 {
		return
	}
	key := string(connectionID)
	s.registry.mu.Lock()
	conn := s.registry.connections[key]
	delete(s.registry.connections, key)
	s.registry.mu.Unlock()
	if conn != nil {
		conn.close()
	}
}

func (s *indexedDBServer) objectStoreFor(connectionID []byte, name string) (coreindexeddb.ObjectStore, func(), error) {
	if err := s.ensureAllowedStore(name); err != nil {
		return nil, func() {}, err
	}
	if db, release, err := s.databaseForConnection(connectionID); err != nil {
		return nil, func() {}, err
	} else if db != nil {
		return metricutil.InstrumentObjectStore(
			db.ObjectStore(s.storeName(name)),
			metricutil.IndexedDBMetricLabels{
				DB:           db.Name(),
				ProviderName: s.plugin,
				ObjectStore:  name,
			},
		), release, nil
	}
	return metricutil.InstrumentObjectStore(
		metricutil.UnwrapIndexedDB(s.ds).ObjectStore(s.storeName(name)),
		metricutil.IndexedDBMetricLabels{
			DB:           s.db,
			ProviderName: s.plugin,
			ObjectStore:  name,
		},
	), func() {}, nil
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

func (s *indexedDBServer) OpenDatabase(stream proto.IndexedDB_OpenDatabaseServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	openReq := first.GetOpen()
	if openReq == nil {
		return status.Error(codes.InvalidArgument, "first message must be OpenDatabaseRequest")
	}
	if s.factory == nil {
		_ = stream.Send(&proto.OpenDatabaseServerMessage{
			Msg: &proto.OpenDatabaseServerMessage_Error{Error: rpcStatusFromError(status.Error(codes.Unimplemented, "indexeddb factory lifecycle is not supported"))},
		})
		return nil
	}
	if err := s.ensureAllowedDatabase(openReq.GetName()); err != nil {
		_ = stream.Send(&proto.OpenDatabaseServerMessage{
			Msg: &proto.OpenDatabaseServerMessage_Error{Error: rpcStatusFromError(err)},
		})
		return nil
	}

	var opened coreindexeddb.Database
	var connectionID []byte
	events := make(chan *proto.OpenDatabaseServerMessage, 16)
	opts := coreindexeddb.OpenOptions{
		Version: openReq.Version,
		Upgrade: func(ctx context.Context, upgrade coreindexeddb.UpgradeContext) error {
			return s.runUpgrade(ctx, stream, upgrade)
		},
		OnVersionChange: func(ctx context.Context, info coreindexeddb.VersionChangeInfo) error {
			msg := &proto.OpenDatabaseServerMessage{
				Msg: &proto.OpenDatabaseServerMessage_Versionchange{Versionchange: versionChangeInfoToProto(info)},
			}
			select {
			case events <- msg:
				return nil
			default:
				if len(connectionID) != 0 {
					go s.closeConnection(connectionID)
				} else if opened != nil {
					_ = opened.Close()
				}
				return nil
			}
		},
		OnBlocked: func(ctx context.Context, info coreindexeddb.BlockedInfo) (coreindexeddb.BlockedAction, error) {
			if err := stream.Send(&proto.OpenDatabaseServerMessage{
				Msg: &proto.OpenDatabaseServerMessage_Blocked{Blocked: blockedInfoToProto(info)},
			}); err != nil {
				return coreindexeddb.BlockedFail, err
			}
			return coreindexeddb.BlockedWait, nil
		},
	}

	var db coreindexeddb.Database
	if openReq.GetRequireExisting() {
		db, err = s.factory.OpenCurrent(stream.Context(), openReq.GetName(), opts)
	} else {
		db, err = s.factory.Open(stream.Context(), openReq.GetName(), opts)
	}
	if err != nil {
		_ = stream.Send(&proto.OpenDatabaseServerMessage{
			Msg: &proto.OpenDatabaseServerMessage_Error{Error: rpcStatusFromError(err)},
		})
		return nil
	}
	opened = db
	connectionID, err = s.registerDatabase(db)
	if err != nil {
		_ = db.Close()
		return err
	}
	defer s.closeConnection(connectionID)

	names, err := db.ObjectStoreNames(stream.Context())
	if err != nil {
		_ = stream.Send(&proto.OpenDatabaseServerMessage{
			Msg: &proto.OpenDatabaseServerMessage_Error{Error: rpcStatusFromError(err)},
		})
		return nil
	}
	names = s.filterStoreNames(names)
	if err := stream.Send(&proto.OpenDatabaseServerMessage{
		Msg: &proto.OpenDatabaseServerMessage_Opened{Opened: &proto.OpenDatabaseSuccess{
			ConnectionId:     connectionID,
			Name:             db.Name(),
			Version:          db.Version(),
			ObjectStoreNames: names,
		}},
	}); err != nil {
		return err
	}

	recvCh := make(chan *proto.OpenDatabaseClientMessage, 1)
	errCh := make(chan error, 1)
	go func() {
		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				errCh <- recvErr
				return
			}
			recvCh <- msg
		}
	}()

	for {
		select {
		case msg := <-events:
			if err := stream.Send(msg); err != nil {
				return err
			}
		case msg := <-recvCh:
			switch msg.GetMsg().(type) {
			case *proto.OpenDatabaseClientMessage_Close:
				if err := stream.Send(&proto.OpenDatabaseServerMessage{
					Msg: &proto.OpenDatabaseServerMessage_Closed{Closed: &proto.CloseDatabaseResponse{}},
				}); err != nil {
					return err
				}
				return nil
			default:
				return status.Error(codes.InvalidArgument, "expected CloseDatabaseRequest after open")
			}
		case recvErr := <-errCh:
			if errors.Is(recvErr, io.EOF) {
				return nil
			}
			return recvErr
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

func (s *indexedDBServer) DeleteDatabase(req *proto.DeleteDatabaseRequest, stream proto.IndexedDB_DeleteDatabaseServer) error {
	if s.factory == nil {
		_ = stream.Send(&proto.DeleteDatabaseServerMessage{
			Msg: &proto.DeleteDatabaseServerMessage_Error{Error: rpcStatusFromError(status.Error(codes.Unimplemented, "indexeddb factory lifecycle is not supported"))},
		})
		return nil
	}
	if err := s.ensureAllowedDatabase(req.GetName()); err != nil {
		_ = stream.Send(&proto.DeleteDatabaseServerMessage{
			Msg: &proto.DeleteDatabaseServerMessage_Error{Error: rpcStatusFromError(err)},
		})
		return nil
	}
	if len(s.allowed) > 0 {
		_ = stream.Send(&proto.DeleteDatabaseServerMessage{
			Msg: &proto.DeleteDatabaseServerMessage_Error{Error: rpcStatusFromError(status.Error(codes.FailedPrecondition, "indexeddb database deletion is not available on store-scoped bindings"))},
		})
		return nil
	}
	result, err := s.factory.DeleteDatabase(stream.Context(), req.GetName(), coreindexeddb.DeleteOptions{
		OnBlocked: func(ctx context.Context, info coreindexeddb.BlockedInfo) (coreindexeddb.BlockedAction, error) {
			if err := stream.Send(&proto.DeleteDatabaseServerMessage{
				Msg: &proto.DeleteDatabaseServerMessage_Blocked{Blocked: blockedInfoToProto(info)},
			}); err != nil {
				return coreindexeddb.BlockedFail, err
			}
			return coreindexeddb.BlockedWait, nil
		},
	})
	if err != nil {
		_ = stream.Send(&proto.DeleteDatabaseServerMessage{
			Msg: &proto.DeleteDatabaseServerMessage_Error{Error: rpcStatusFromError(err)},
		})
		return nil
	}
	return stream.Send(&proto.DeleteDatabaseServerMessage{
		Msg: &proto.DeleteDatabaseServerMessage_Deleted{Deleted: &proto.DeleteDatabaseResponse{
			Name:       result.Name,
			OldVersion: result.OldVersion,
		}},
	})
}

func (s *indexedDBServer) Databases(ctx context.Context, _ *proto.DatabasesRequest) (*proto.DatabasesResponse, error) {
	if s.factory == nil {
		return nil, status.Error(codes.Unimplemented, "indexeddb factory lifecycle is not supported")
	}
	infos, err := s.factory.Databases(ctx)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	resp := &proto.DatabasesResponse{Databases: make([]*proto.DatabaseInfo, 0, len(infos))}
	for _, info := range infos {
		if err := s.ensureAllowedDatabase(info.Name); err != nil {
			continue
		}
		resp.Databases = append(resp.Databases, &proto.DatabaseInfo{Name: info.Name, Version: info.Version})
	}
	return resp, nil
}

func (s *indexedDBServer) CompareKeys(ctx context.Context, req *proto.CompareKeysRequest) (*proto.CompareKeysResponse, error) {
	if s.factory == nil {
		return nil, status.Error(codes.Unimplemented, "indexeddb factory lifecycle is not supported")
	}
	first, err := keyValueToAny(req.GetFirst())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal first key: %v", err)
	}
	second, err := keyValueToAny(req.GetSecond())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal second key: %v", err)
	}
	cmp, err := s.factory.CompareKeys(first, second)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.CompareKeysResponse{Cmp: int32(cmp)}, nil
}

func (s *indexedDBServer) runUpgrade(ctx context.Context, stream proto.IndexedDB_OpenDatabaseServer, upgrade coreindexeddb.UpgradeContext) error {
	names, err := upgrade.ObjectStoreNames(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&proto.OpenDatabaseServerMessage{
		Msg: &proto.OpenDatabaseServerMessage_UpgradeStarted{UpgradeStarted: &proto.UpgradeStarted{
			Name:             upgrade.Database().Name(),
			OldVersion:       upgrade.OldVersion(),
			NewVersion:       upgrade.NewVersion(),
			ObjectStoreNames: s.filterStoreNames(names),
		}},
	}); err != nil {
		return err
	}
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		op := msg.GetUpgradeOperation()
		if op == nil {
			if msg.GetClose() != nil {
				return coreindexeddb.ErrAbort
			}
			return status.Error(codes.InvalidArgument, "expected UpgradeOperation during upgrade")
		}
		resp, terminal, opErr := s.executeUpgradeOperation(ctx, upgrade, op)
		if err := stream.Send(&proto.OpenDatabaseServerMessage{
			Msg: &proto.OpenDatabaseServerMessage_UpgradeOperationResponse{UpgradeOperationResponse: resp},
		}); err != nil {
			return err
		}
		if opErr != nil {
			return opErr
		}
		if terminal {
			return nil
		}
	}
}

func (s *indexedDBServer) executeUpgradeOperation(ctx context.Context, upgrade coreindexeddb.UpgradeContext, op *proto.UpgradeOperation) (*proto.UpgradeOperationResponse, bool, error) {
	resp := &proto.UpgradeOperationResponse{RequestId: op.GetRequestId()}
	var err error
	terminal := false
	switch body := op.GetOp().(type) {
	case *proto.UpgradeOperation_CreateObjectStore:
		if err = s.ensureAllowedStore(body.CreateObjectStore.GetName()); err == nil {
			err = upgrade.CreateObjectStore(ctx, s.storeName(body.CreateObjectStore.GetName()), protoToSchema(body.CreateObjectStore.GetSchema()))
		}
	case *proto.UpgradeOperation_DeleteObjectStore:
		if err = s.ensureAllowedStore(body.DeleteObjectStore.GetName()); err == nil {
			err = upgrade.DeleteObjectStore(ctx, s.storeName(body.DeleteObjectStore.GetName()))
		}
	case *proto.UpgradeOperation_CreateIndex:
		if err = s.ensureAllowedStore(body.CreateIndex.GetStore()); err == nil {
			err = upgrade.CreateIndex(ctx, s.storeName(body.CreateIndex.GetStore()), coreindexeddb.IndexSchema{
				Name:    body.CreateIndex.GetName(),
				KeyPath: body.CreateIndex.GetKeyPath(),
				Unique:  body.CreateIndex.GetUnique(),
			})
		}
	case *proto.UpgradeOperation_DeleteIndex:
		if err = s.ensureAllowedStore(body.DeleteIndex.GetStore()); err == nil {
			err = upgrade.DeleteIndex(ctx, s.storeName(body.DeleteIndex.GetStore()), body.DeleteIndex.GetName())
		}
	case *proto.UpgradeOperation_ObjectStoreNames:
		var names []string
		names, err = upgrade.ObjectStoreNames(ctx)
		resp.ObjectStoreNames = s.filterStoreNames(names)
	case *proto.UpgradeOperation_FinishUpgrade:
		terminal = true
	case *proto.UpgradeOperation_AbortUpgrade:
		err = fmt.Errorf("%w: %s", coreindexeddb.ErrAbort, body.AbortUpgrade.GetReason())
		terminal = true
	default:
		err = status.Error(codes.InvalidArgument, "unknown upgrade operation")
		terminal = true
	}
	if err != nil {
		resp.Error = rpcStatusFromError(err)
		if terminal {
			return resp, true, err
		}
		return resp, false, nil
	}
	return resp, terminal, nil
}

func (s *indexedDBServer) Get(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
	rec, err := store.Get(ctx, req.GetId())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordResponseFromRecord(rec)
}

func (s *indexedDBServer) GetKey(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.KeyResponse, error) {
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
	if err := store.Put(ctx, record); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Delete(ctx context.Context, req *proto.ObjectStoreRequest) (*emptypb.Empty, error) {
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
	if err := store.Delete(ctx, req.GetId()); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Clear(ctx context.Context, req *proto.ObjectStoreNameRequest) (*emptypb.Empty, error) {
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
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
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
	count, err := store.Index(req.GetIndex()).Count(ctx, keyRange, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) IndexDelete(ctx context.Context, req *proto.IndexQueryRequest) (*proto.DeleteResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	store, release, err := s.objectStoreFor(req.GetConnectionId(), req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	defer release()
	deleted, err := store.Index(req.GetIndex()).DeleteRange(ctx, keyRange, values...)
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
	var tx coreindexeddb.Transaction
	if db, release, connErr := s.databaseForConnection(begin.GetConnectionId()); connErr != nil {
		return indexeddbToGRPCErr(connErr)
	} else if db != nil {
		defer release()
		tx, err = db.Transaction(
			stream.Context(),
			stores,
			protoTransactionMode(begin.GetMode()),
			coreindexeddb.TransactionOptions{DurabilityHint: protoDurabilityHint(begin.GetDurabilityHint())},
		)
	} else {
		tx, err = metricutil.UnwrapIndexedDB(s.ds).Transaction(
			stream.Context(),
			stores,
			protoTransactionMode(begin.GetMode()),
			coreindexeddb.TransactionOptions{DurabilityHint: protoDurabilityHint(begin.GetDurabilityHint())},
		)
	}
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
	idx, values, keyRange, err := s.transactionIndex(ctx, tx, req)
	if err != nil {
		return 0, err
	}
	return idx.DeleteRange(ctx, keyRange, values...)
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

func versionChangeInfoToProto(info coreindexeddb.VersionChangeInfo) *proto.VersionChangeInfo {
	return &proto.VersionChangeInfo{
		Name:       info.Name,
		OldVersion: info.OldVersion,
		NewVersion: info.NewVersion,
		Reason:     versionChangeReasonToProto(info.Reason),
	}
}

func blockedInfoToProto(info coreindexeddb.BlockedInfo) *proto.BlockedInfo {
	return &proto.BlockedInfo{
		Name:             info.Name,
		OldVersion:       info.OldVersion,
		NewVersion:       info.NewVersion,
		Reason:           versionChangeReasonToProto(info.Reason),
		OpenConnections:  int32(info.OpenConnections),
		ActiveOperations: int32(info.ActiveTransactions),
	}
}

func versionChangeReasonToProto(reason coreindexeddb.VersionChangeReason) proto.VersionChangeReason {
	switch reason {
	case coreindexeddb.VersionChangeDelete:
		return proto.VersionChangeReason_VERSION_CHANGE_REASON_DELETE
	case coreindexeddb.VersionChangeUpgrade:
		return proto.VersionChangeReason_VERSION_CHANGE_REASON_UPGRADE
	default:
		return proto.VersionChangeReason_VERSION_CHANGE_REASON_UNSPECIFIED
	}
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
	store, release, err := s.objectStoreFor(openReq.GetConnectionId(), openReq.GetStore())
	if err != nil {
		return indexeddbToGRPCErr(err)
	}
	defer release()

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
	if _, ok := status.FromError(err); ok {
		return err
	}
	if errors.Is(err, coreindexeddb.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, coreindexeddb.ErrAlreadyExists) {
		return status.Error(codes.AlreadyExists, err.Error())
	}
	if errors.Is(err, coreindexeddb.ErrAbort) {
		return status.Error(codes.Canceled, err.Error())
	}
	if errors.Is(err, coreindexeddb.ErrBlocked) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	if errors.Is(err, coreindexeddb.ErrInvalidTransaction) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if errors.Is(err, coreindexeddb.ErrReadOnly) || errors.Is(err, coreindexeddb.ErrTransactionDone) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}
