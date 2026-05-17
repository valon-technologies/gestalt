package gestalt

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const indexedDBLifecycleEventQueueSize = 16

// ServeIndexedDBProvider starts a gRPC server for an [IndexedDBProvider].
// Store/index/cursor/transaction operations are scoped to opened database
// handles; no-connection store operations are rejected.
func ServeIndexedDBProvider(ctx context.Context, provider IndexedDBProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindIndexedDB, provider))
		proto.RegisterIndexedDBServer(srv, newIndexedDBProviderServer(provider))
	})
}

type indexedDBProviderServer struct {
	proto.UnimplementedIndexedDBServer
	provider IndexedDBProvider

	mu          sync.Mutex
	connections map[string]*indexedDBProviderConnection
}

func newIndexedDBProviderServer(provider IndexedDBProvider) *indexedDBProviderServer {
	return &indexedDBProviderServer{
		provider:    provider,
		connections: make(map[string]*indexedDBProviderConnection),
	}
}

type indexedDBProviderConnection struct {
	db      IndexedDBDatabase
	mu      sync.Mutex
	cond    *sync.Cond
	active  int
	closing bool
}

func newIndexedDBProviderConnection(db IndexedDBDatabase) *indexedDBProviderConnection {
	conn := &indexedDBProviderConnection{db: db}
	conn.cond = sync.NewCond(&conn.mu)
	return conn
}

func (c *indexedDBProviderConnection) acquire() (IndexedDBDatabase, func(), error) {
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return nil, nil, status.Error(codes.FailedPrecondition, "indexeddb database connection is closing")
	}
	c.active++
	c.mu.Unlock()
	return c.db, c.release, nil
}

func (c *indexedDBProviderConnection) release() {
	c.mu.Lock()
	c.active--
	if c.active == 0 {
		c.cond.Broadcast()
	}
	c.mu.Unlock()
}

func (c *indexedDBProviderConnection) close() {
	c.mu.Lock()
	c.closing = true
	for c.active > 0 {
		c.cond.Wait()
	}
	c.mu.Unlock()
	_ = c.db.Close()
}

func (s *indexedDBProviderServer) registerDatabase(db IndexedDBDatabase) ([]byte, error) {
	for range 8 {
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return nil, status.Errorf(codes.Internal, "generate indexeddb connection id: %v", err)
		}
		key := string(id)
		s.mu.Lock()
		if _, exists := s.connections[key]; !exists {
			s.connections[key] = newIndexedDBProviderConnection(db)
			s.mu.Unlock()
			return id, nil
		}
		s.mu.Unlock()
	}
	return nil, status.Error(codes.Internal, "generate indexeddb connection id: collision")
}

func (s *indexedDBProviderServer) databaseForConnection(connectionID []byte) (IndexedDBDatabase, func(), error) {
	if len(connectionID) == 0 {
		return nil, nil, status.Error(codes.FailedPrecondition, "indexeddb database connection_id is required")
	}
	s.mu.Lock()
	conn := s.connections[string(connectionID)]
	s.mu.Unlock()
	if conn == nil {
		return nil, nil, status.Error(codes.NotFound, "indexeddb database connection is closed")
	}
	return conn.acquire()
}

func (s *indexedDBProviderServer) closeConnection(connectionID []byte) {
	s.mu.Lock()
	conn := s.connections[string(connectionID)]
	delete(s.connections, string(connectionID))
	s.mu.Unlock()
	if conn != nil {
		conn.close()
	}
}

func (s *indexedDBProviderServer) CreateObjectStore(context.Context, *proto.CreateObjectStoreRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.FailedPrecondition, "indexeddb object stores must be created during an open database upgrade")
}

func (s *indexedDBProviderServer) DeleteObjectStore(context.Context, *proto.DeleteObjectStoreRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.FailedPrecondition, "indexeddb object stores must be deleted during an open database upgrade")
}

func (s *indexedDBProviderServer) OpenDatabase(stream proto.IndexedDB_OpenDatabaseServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	openReq := first.GetOpen()
	if openReq == nil {
		return status.Error(codes.InvalidArgument, "first message must be OpenDatabaseRequest")
	}

	var opened IndexedDBDatabase
	var connectionID []byte
	events := make(chan *proto.OpenDatabaseServerMessage, indexedDBLifecycleEventQueueSize)
	opts := OpenOptions{
		Version: openReq.Version,
		Upgrade: func(ctx context.Context, upgrade UpgradeContext) error {
			return s.runUpgrade(ctx, stream, upgrade)
		},
		OnVersionChange: func(ctx context.Context, info VersionChangeInfo) error {
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
		OnBlocked: func(ctx context.Context, info BlockedInfo) (BlockedAction, error) {
			if err := stream.Send(&proto.OpenDatabaseServerMessage{
				Msg: &proto.OpenDatabaseServerMessage_Blocked{Blocked: blockedInfoToProto(info)},
			}); err != nil {
				return BlockedFail, err
			}
			return BlockedWait, nil
		},
	}

	var db IndexedDBDatabase
	if openReq.GetRequireExisting() {
		db, err = s.provider.OpenCurrentDatabase(stream.Context(), openReq.GetName(), opts)
	} else {
		db, err = s.provider.OpenDatabase(stream.Context(), openReq.GetName(), opts)
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

func (s *indexedDBProviderServer) DeleteDatabase(req *proto.DeleteDatabaseRequest, stream proto.IndexedDB_DeleteDatabaseServer) error {
	result, err := s.provider.DeleteDatabase(stream.Context(), req.GetName(), DeleteOptions{
		OnBlocked: func(ctx context.Context, info BlockedInfo) (BlockedAction, error) {
			if err := stream.Send(&proto.DeleteDatabaseServerMessage{
				Msg: &proto.DeleteDatabaseServerMessage_Blocked{Blocked: blockedInfoToProto(info)},
			}); err != nil {
				return BlockedFail, err
			}
			return BlockedWait, nil
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

func (s *indexedDBProviderServer) Databases(ctx context.Context, _ *proto.DatabasesRequest) (*proto.DatabasesResponse, error) {
	infos, err := s.provider.Databases(ctx)
	if err != nil {
		return nil, providerRPCError("indexeddb databases", err)
	}
	resp := &proto.DatabasesResponse{Databases: make([]*proto.DatabaseInfo, len(infos))}
	for i, info := range infos {
		resp.Databases[i] = &proto.DatabaseInfo{Name: info.Name, Version: info.Version}
	}
	return resp, nil
}

func (s *indexedDBProviderServer) CompareKeys(ctx context.Context, req *proto.CompareKeysRequest) (*proto.CompareKeysResponse, error) {
	first, err := keyValueToAny(req.GetFirst())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal first key: %v", err)
	}
	second, err := keyValueToAny(req.GetSecond())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal second key: %v", err)
	}
	cmp, err := s.provider.CompareKeys(ctx, first, second)
	if err != nil {
		return nil, providerRPCError("indexeddb compare keys", err)
	}
	return &proto.CompareKeysResponse{Cmp: int32(cmp)}, nil
}

func (s *indexedDBProviderServer) runUpgrade(ctx context.Context, stream proto.IndexedDB_OpenDatabaseServer, upgrade UpgradeContext) error {
	names, err := upgrade.ObjectStoreNames(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&proto.OpenDatabaseServerMessage{
		Msg: &proto.OpenDatabaseServerMessage_UpgradeStarted{UpgradeStarted: &proto.UpgradeStarted{
			Name:             upgrade.Database().Name(),
			OldVersion:       upgrade.OldVersion(),
			NewVersion:       upgrade.NewVersion(),
			ObjectStoreNames: names,
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
				return ErrAbort
			}
			return status.Error(codes.InvalidArgument, "expected UpgradeOperation during upgrade")
		}
		resp, terminal, opErr := executeIndexedDBUpgradeOperation(ctx, upgrade, op)
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

func executeIndexedDBUpgradeOperation(ctx context.Context, upgrade UpgradeContext, op *proto.UpgradeOperation) (*proto.UpgradeOperationResponse, bool, error) {
	resp := &proto.UpgradeOperationResponse{RequestId: op.GetRequestId()}
	terminal := false
	var err error
	switch body := op.GetOp().(type) {
	case *proto.UpgradeOperation_CreateObjectStore:
		err = upgrade.CreateObjectStore(ctx, body.CreateObjectStore.GetName(), objectStoreSchemaFromProto(body.CreateObjectStore.GetSchema()))
	case *proto.UpgradeOperation_DeleteObjectStore:
		err = upgrade.DeleteObjectStore(ctx, body.DeleteObjectStore.GetName())
	case *proto.UpgradeOperation_CreateIndex:
		err = upgrade.CreateIndex(ctx, body.CreateIndex.GetStore(), IndexSchema{
			Name:    body.CreateIndex.GetName(),
			KeyPath: body.CreateIndex.GetKeyPath(),
			Unique:  body.CreateIndex.GetUnique(),
		})
	case *proto.UpgradeOperation_DeleteIndex:
		err = upgrade.DeleteIndex(ctx, body.DeleteIndex.GetStore(), body.DeleteIndex.GetName())
	case *proto.UpgradeOperation_ObjectStoreNames:
		resp.ObjectStoreNames, err = upgrade.ObjectStoreNames(ctx)
	case *proto.UpgradeOperation_FinishUpgrade:
		terminal = true
	case *proto.UpgradeOperation_AbortUpgrade:
		err = fmt.Errorf("%w: %s", ErrAbort, body.AbortUpgrade.GetReason())
	default:
		err = status.Error(codes.InvalidArgument, "unknown upgrade operation")
	}
	if err != nil {
		resp.Error = rpcStatusFromError(err)
		return resp, true, err
	}
	return resp, terminal, nil
}

func (s *indexedDBProviderServer) Get(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	record, err := db.Get(ctx, objectStoreRequestFromProto(req))
	return recordResponseToProto("indexeddb get", record, err)
}

func (s *indexedDBProviderServer) GetKey(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.KeyResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	key, err := db.GetKey(ctx, objectStoreRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("indexeddb get key", err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBProviderServer) Add(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	recordReq, err := recordRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return &emptypb.Empty{}, providerRPCError("indexeddb add", db.Add(ctx, recordReq))
}

func (s *indexedDBProviderServer) Put(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	recordReq, err := recordRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return &emptypb.Empty{}, providerRPCError("indexeddb put", db.Put(ctx, recordReq))
}

func (s *indexedDBProviderServer) Delete(ctx context.Context, req *proto.ObjectStoreRequest) (*emptypb.Empty, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	return &emptypb.Empty{}, providerRPCError("indexeddb delete", db.Delete(ctx, objectStoreRequestFromProto(req)))
}

func (s *indexedDBProviderServer) Clear(ctx context.Context, req *proto.ObjectStoreNameRequest) (*emptypb.Empty, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	return &emptypb.Empty{}, providerRPCError("indexeddb clear", db.Clear(ctx, req.GetStore()))
}

func (s *indexedDBProviderServer) GetAll(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.RecordsResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	records, err := db.GetAll(ctx, objectStoreRangeRequestFromProto(req))
	return recordsResponseToProto("indexeddb get all", records, err)
}

func (s *indexedDBProviderServer) GetAllKeys(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.KeysResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	keys, err := db.GetAllKeys(ctx, objectStoreRangeRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("indexeddb get all keys", err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBProviderServer) Count(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.CountResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	count, err := db.Count(ctx, objectStoreRangeRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("indexeddb count", err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBProviderServer) DeleteRange(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.DeleteResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	deleted, err := db.DeleteRange(ctx, objectStoreRangeRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("indexeddb delete range", err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s *indexedDBProviderServer) IndexGet(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	record, err := db.IndexGet(ctx, query)
	return recordResponseToProto("indexeddb index get", record, err)
}

func (s *indexedDBProviderServer) IndexGetKey(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeyResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	key, err := db.IndexGetKey(ctx, query)
	if err != nil {
		return nil, providerRPCError("indexeddb index get key", err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBProviderServer) IndexGetAll(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordsResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	records, err := db.IndexGetAll(ctx, query)
	return recordsResponseToProto("indexeddb index get all", records, err)
}

func (s *indexedDBProviderServer) IndexGetAllKeys(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeysResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	keys, err := db.IndexGetAllKeys(ctx, query)
	if err != nil {
		return nil, providerRPCError("indexeddb index get all keys", err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBProviderServer) IndexCount(ctx context.Context, req *proto.IndexQueryRequest) (*proto.CountResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	count, err := db.IndexCount(ctx, query)
	if err != nil {
		return nil, providerRPCError("indexeddb index count", err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBProviderServer) IndexDelete(ctx context.Context, req *proto.IndexQueryRequest) (*proto.DeleteResponse, error) {
	db, release, err := s.databaseForConnection(req.GetConnectionId())
	if err != nil {
		return nil, err
	}
	defer release()
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	deleted, err := db.IndexDelete(ctx, query)
	if err != nil {
		return nil, providerRPCError("indexeddb index delete", err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s *indexedDBProviderServer) OpenCursor(stream grpc.BidiStreamingServer[proto.CursorClientMessage, proto.CursorResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	openReq := first.GetOpen()
	if openReq == nil {
		return status.Error(codes.InvalidArgument, "first message must be OpenCursorRequest")
	}
	db, release, err := s.databaseForConnection(openReq.GetConnectionId())
	if err != nil {
		return err
	}
	defer release()
	req, err := openCursorRequestFromProto(openReq)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "%v", err)
	}
	cursor, err := db.OpenCursor(stream.Context(), req)
	if err != nil {
		return providerRPCError("indexeddb open cursor", err)
	}
	defer func() { _ = cursor.Close() }()
	if err := stream.Send(cursorDoneResponse(false)); err != nil {
		return err
	}

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		cmd := msg.GetCommand()
		if cmd == nil {
			return status.Error(codes.InvalidArgument, "expected CursorCommand after open")
		}
		switch v := cmd.GetCommand().(type) {
		case *proto.CursorCommand_Next:
			entry, err := cursor.Next(stream.Context())
			if err := sendCursorResult(stream, entry, req.Index != "", err); err != nil {
				return err
			}
		case *proto.CursorCommand_ContinueToKey:
			target, err := cursorTargetFromProto(v.ContinueToKey.GetKey(), req.Index != "")
			if err != nil {
				return status.Errorf(codes.InvalidArgument, "unmarshal cursor target: %v", err)
			}
			entry, err := cursor.ContinueToKey(stream.Context(), target)
			if err := sendCursorResult(stream, entry, req.Index != "", err); err != nil {
				return err
			}
		case *proto.CursorCommand_Advance:
			entry, err := cursor.Advance(stream.Context(), int(v.Advance))
			if err := sendCursorResult(stream, entry, req.Index != "", err); err != nil {
				return err
			}
		case *proto.CursorCommand_Delete:
			if err := cursor.Delete(stream.Context()); err != nil {
				return providerRPCError("indexeddb cursor delete", err)
			}
			if err := stream.Send(cursorDoneResponse(false)); err != nil {
				return err
			}
		case *proto.CursorCommand_Update:
			record, err := recordFromProto(v.Update)
			if err != nil {
				return status.Errorf(codes.InvalidArgument, "unmarshal cursor update: %v", err)
			}
			entry, err := cursor.Update(stream.Context(), record)
			if err := sendCursorResult(stream, entry, req.Index != "", err); err != nil {
				return err
			}
		case *proto.CursorCommand_Close:
			return nil
		default:
			return status.Error(codes.InvalidArgument, "unknown cursor command")
		}
	}
}

func (s *indexedDBProviderServer) Transaction(stream proto.IndexedDB_TransactionServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	beginReq := first.GetBegin()
	if beginReq == nil {
		return status.Error(codes.InvalidArgument, "first message must be BeginTransactionRequest")
	}
	db, release, err := s.databaseForConnection(beginReq.GetConnectionId())
	if err != nil {
		return err
	}
	defer release()
	if len(beginReq.GetStores()) == 0 {
		return status.Error(codes.InvalidArgument, "invalid transaction: at least one object store is required")
	}
	req := IndexedDBBeginTransactionRequest{
		Stores:         beginReq.GetStores(),
		Mode:           transactionModeFromProto(beginReq.GetMode()),
		DurabilityHint: durabilityHintFromProto(beginReq.GetDurabilityHint()),
	}
	tx, err := db.BeginTransaction(stream.Context(), req)
	if err != nil {
		return providerRPCError("indexeddb begin transaction", err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = tx.Abort(stream.Context())
		}
	}()
	if err := stream.Send(&proto.TransactionServerMessage{Msg: &proto.TransactionServerMessage_Begin{Begin: &proto.TransactionBeginResponse{}}}); err != nil {
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
			opErr := readonlyOperationError(req.Mode, body.Operation)
			resp := (*proto.TransactionOperationResponse)(nil)
			if opErr == nil {
				resp, opErr = executeIndexedDBOperation(stream.Context(), tx, body.Operation)
			}
			if opErr != nil {
				finished = true
				abortErr := tx.Abort(stream.Context())
				if err := stream.Send(&proto.TransactionServerMessage{Msg: &proto.TransactionServerMessage_Operation{Operation: transactionOperationError(body.Operation.GetRequestId(), opErr)}}); err != nil {
					return err
				}
				if err := stream.Send(&proto.TransactionServerMessage{Msg: &proto.TransactionServerMessage_Abort{Abort: &proto.TransactionAbortResponse{Error: rpcStatusFromError(abortErr)}}}); err != nil {
					return err
				}
				return drainIndexedDBTransaction(stream)
			}
			if err := stream.Send(&proto.TransactionServerMessage{Msg: &proto.TransactionServerMessage_Operation{Operation: resp}}); err != nil {
				return err
			}
		case *proto.TransactionClientMessage_Commit:
			finished = true
			commitErr := tx.Commit(stream.Context())
			return stream.Send(&proto.TransactionServerMessage{Msg: &proto.TransactionServerMessage_Commit{Commit: &proto.TransactionCommitResponse{Error: rpcStatusFromError(commitErr)}}})
		case *proto.TransactionClientMessage_Abort:
			finished = true
			abortErr := tx.Abort(stream.Context())
			return stream.Send(&proto.TransactionServerMessage{Msg: &proto.TransactionServerMessage_Abort{Abort: &proto.TransactionAbortResponse{Error: rpcStatusFromError(abortErr)}}})
		default:
			finished = true
			_ = tx.Abort(stream.Context())
			return status.Error(codes.InvalidArgument, "expected transaction operation, commit, or abort")
		}
	}
}

func versionChangeInfoToProto(info VersionChangeInfo) *proto.VersionChangeInfo {
	return &proto.VersionChangeInfo{
		Name:       info.Name,
		OldVersion: info.OldVersion,
		NewVersion: info.NewVersion,
		Reason:     versionChangeReasonToProto(info.Reason),
	}
}

func blockedInfoToProto(info BlockedInfo) *proto.BlockedInfo {
	return &proto.BlockedInfo{
		Name:             info.Name,
		OldVersion:       info.OldVersion,
		NewVersion:       info.NewVersion,
		Reason:           versionChangeReasonToProto(info.Reason),
		OpenConnections:  int32(info.OpenConnections),
		ActiveOperations: int32(info.ActiveTransactions),
	}
}

func versionChangeReasonToProto(reason VersionChangeReason) proto.VersionChangeReason {
	switch reason {
	case VersionChangeDelete:
		return proto.VersionChangeReason_VERSION_CHANGE_REASON_DELETE
	case VersionChangeUpgrade:
		return proto.VersionChangeReason_VERSION_CHANGE_REASON_UPGRADE
	default:
		return proto.VersionChangeReason_VERSION_CHANGE_REASON_UNSPECIFIED
	}
}
