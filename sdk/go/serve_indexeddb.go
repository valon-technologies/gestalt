package gestalt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ServeIndexedDBProvider starts a gRPC server for an [IndexedDBProvider].
func ServeIndexedDBProvider(ctx context.Context, datastore IndexedDBProvider) error {
	return serveProvider(withProviderCloser(ctx, datastore), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindIndexedDB, datastore))
		proto.RegisterIndexedDBServer(srv, indexedDBProviderServer{provider: datastore})
	})
}

type indexedDBProviderServer struct {
	proto.UnimplementedIndexedDBServer
	provider IndexedDBProvider
}

func (s indexedDBProviderServer) CreateObjectStore(ctx context.Context, req *proto.CreateObjectStoreRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("indexeddb create object store", s.provider.CreateObjectStore(ctx, req.GetName(), objectStoreSchemaFromProto(req.GetSchema())))
}

func (s indexedDBProviderServer) DeleteObjectStore(ctx context.Context, req *proto.DeleteObjectStoreRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("indexeddb delete object store", s.provider.DeleteObjectStore(ctx, req.GetName()))
}

func (s indexedDBProviderServer) Get(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	record, err := s.provider.Get(ctx, objectStoreRequestFromProto(req))
	return recordResponseToProto("indexeddb get", record, err)
}

func (s indexedDBProviderServer) GetKey(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.KeyResponse, error) {
	key, err := s.provider.GetKey(ctx, objectStoreRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("indexeddb get key", err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s indexedDBProviderServer) Add(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	record, err := recordFromProto(req.GetRecord())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal record: %v", err)
	}
	return &emptypb.Empty{}, providerRPCError("indexeddb add", s.provider.Add(ctx, IndexedDBRecordRequest{Store: req.GetStore(), Record: record}))
}

func (s indexedDBProviderServer) Put(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	record, err := recordFromProto(req.GetRecord())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal record: %v", err)
	}
	return &emptypb.Empty{}, providerRPCError("indexeddb put", s.provider.Put(ctx, IndexedDBRecordRequest{Store: req.GetStore(), Record: record}))
}

func (s indexedDBProviderServer) Delete(ctx context.Context, req *proto.ObjectStoreRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("indexeddb delete", s.provider.Delete(ctx, objectStoreRequestFromProto(req)))
}

func (s indexedDBProviderServer) Clear(ctx context.Context, req *proto.ObjectStoreNameRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("indexeddb clear", s.provider.Clear(ctx, req.GetStore()))
}

func (s indexedDBProviderServer) GetAll(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.RecordsResponse, error) {
	records, err := s.provider.GetAll(ctx, objectStoreRangeRequestFromProto(req))
	return recordsResponseToProto("indexeddb get all", records, err)
}

func (s indexedDBProviderServer) GetAllKeys(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.KeysResponse, error) {
	keys, err := s.provider.GetAllKeys(ctx, objectStoreRangeRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("indexeddb get all keys", err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s indexedDBProviderServer) Count(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.CountResponse, error) {
	count, err := s.provider.Count(ctx, objectStoreRangeRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("indexeddb count", err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s indexedDBProviderServer) DeleteRange(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.DeleteResponse, error) {
	deleted, err := s.provider.DeleteRange(ctx, objectStoreRangeRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("indexeddb delete range", err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s indexedDBProviderServer) IndexGet(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordResponse, error) {
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	record, err := s.provider.IndexGet(ctx, query)
	return recordResponseToProto("indexeddb index get", record, err)
}

func (s indexedDBProviderServer) IndexGetKey(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeyResponse, error) {
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	key, err := s.provider.IndexGetKey(ctx, query)
	if err != nil {
		return nil, providerRPCError("indexeddb index get key", err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s indexedDBProviderServer) IndexGetAll(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordsResponse, error) {
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	records, err := s.provider.IndexGetAll(ctx, query)
	return recordsResponseToProto("indexeddb index get all", records, err)
}

func (s indexedDBProviderServer) IndexGetAllKeys(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeysResponse, error) {
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	keys, err := s.provider.IndexGetAllKeys(ctx, query)
	if err != nil {
		return nil, providerRPCError("indexeddb index get all keys", err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s indexedDBProviderServer) IndexCount(ctx context.Context, req *proto.IndexQueryRequest) (*proto.CountResponse, error) {
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	count, err := s.provider.IndexCount(ctx, query)
	if err != nil {
		return nil, providerRPCError("indexeddb index count", err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s indexedDBProviderServer) IndexDelete(ctx context.Context, req *proto.IndexQueryRequest) (*proto.DeleteResponse, error) {
	query, err := indexQueryRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	deleted, err := s.provider.IndexDelete(ctx, query)
	if err != nil {
		return nil, providerRPCError("indexeddb index delete", err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s indexedDBProviderServer) OpenCursor(stream grpc.BidiStreamingServer[proto.CursorClientMessage, proto.CursorResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	openReq := first.GetOpen()
	if openReq == nil {
		return status.Error(codes.InvalidArgument, "first message must be OpenCursorRequest")
	}
	req, err := openCursorRequestFromProto(openReq)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "%v", err)
	}
	cursor, err := s.provider.OpenCursor(stream.Context(), req)
	if err != nil {
		return providerRPCError("indexeddb open cursor", err)
	}
	defer cursor.Close()
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

func (s indexedDBProviderServer) Transaction(stream proto.IndexedDB_TransactionServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	beginReq := first.GetBegin()
	if beginReq == nil {
		return status.Error(codes.InvalidArgument, "first message must be BeginTransactionRequest")
	}
	if len(beginReq.GetStores()) == 0 {
		return status.Error(codes.InvalidArgument, "invalid transaction: at least one object store is required")
	}
	req := IndexedDBBeginTransactionRequest{
		Stores:         beginReq.GetStores(),
		Mode:           transactionModeFromProto(beginReq.GetMode()),
		DurabilityHint: durabilityHintFromProto(beginReq.GetDurabilityHint()),
	}
	tx, err := s.provider.BeginTransaction(stream.Context(), req)
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

func objectStoreSchemaFromProto(schema *proto.ObjectStoreSchema) ObjectStoreSchema {
	if schema == nil {
		return ObjectStoreSchema{}
	}
	out := ObjectStoreSchema{
		Indexes: make([]IndexSchema, len(schema.GetIndexes())),
		Columns: make([]ColumnDef, len(schema.GetColumns())),
	}
	for i, idx := range schema.GetIndexes() {
		out.Indexes[i] = IndexSchema{Name: idx.GetName(), KeyPath: idx.GetKeyPath(), Unique: idx.GetUnique()}
	}
	for i, col := range schema.GetColumns() {
		out.Columns[i] = ColumnDef{
			Name:       col.GetName(),
			Type:       ColumnType(col.GetType()),
			PrimaryKey: col.GetPrimaryKey(),
			NotNull:    col.GetNotNull(),
			Unique:     col.GetUnique(),
		}
	}
	return out
}

func objectStoreSchemaToProto(schema ObjectStoreSchema) *proto.ObjectStoreSchema {
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
	return &proto.ObjectStoreSchema{Indexes: indexes, Columns: columns}
}

func objectStoreRequestFromProto(req *proto.ObjectStoreRequest) IndexedDBObjectStoreRequest {
	return IndexedDBObjectStoreRequest{Store: req.GetStore(), ID: req.GetId()}
}

func objectStoreRangeRequestFromProto(req *proto.ObjectStoreRangeRequest) IndexedDBObjectStoreRangeRequest {
	return IndexedDBObjectStoreRangeRequest{Store: req.GetStore(), Range: keyRangeFromProto(req.GetRange())}
}

func indexQueryRequestFromProto(req *proto.IndexQueryRequest) (IndexedDBIndexQueryRequest, error) {
	values, err := anyFromTypedValues(req.GetValues())
	if err != nil {
		return IndexedDBIndexQueryRequest{}, fmt.Errorf("unmarshal index values: %w", err)
	}
	return IndexedDBIndexQueryRequest{Store: req.GetStore(), Index: req.GetIndex(), Values: values, Range: keyRangeFromProto(req.GetRange())}, nil
}

func openCursorRequestFromProto(req *proto.OpenCursorRequest) (IndexedDBOpenCursorRequest, error) {
	values, err := anyFromTypedValues(req.GetValues())
	if err != nil {
		return IndexedDBOpenCursorRequest{}, fmt.Errorf("unmarshal cursor values: %w", err)
	}
	return IndexedDBOpenCursorRequest{
		Store:     req.GetStore(),
		Range:     keyRangeFromProto(req.GetRange()),
		Direction: cursorDirectionFromProto(req.GetDirection()),
		KeysOnly:  req.GetKeysOnly(),
		Index:     req.GetIndex(),
		Values:    values,
	}, nil
}

func keyRangeFromProto(r *proto.KeyRange) *KeyRange {
	if r == nil {
		return nil
	}
	out := &KeyRange{LowerOpen: r.GetLowerOpen(), UpperOpen: r.GetUpperOpen()}
	if r.GetLower() != nil {
		out.Lower, _ = anyFromTypedValue(r.GetLower())
	}
	if r.GetUpper() != nil {
		out.Upper, _ = anyFromTypedValue(r.GetUpper())
	}
	return out
}

func recordResponseToProto(operation string, record Record, err error) (*proto.RecordResponse, error) {
	if err != nil {
		return nil, providerRPCError(operation, err)
	}
	pbRecord, err := recordToProto(record)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal record: %v", err)
	}
	return &proto.RecordResponse{Record: pbRecord}, nil
}

func recordsResponseToProto(operation string, records []Record, err error) (*proto.RecordsResponse, error) {
	if err != nil {
		return nil, providerRPCError(operation, err)
	}
	pbRecords, err := recordsToProto(records)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal records: %v", err)
	}
	return &proto.RecordsResponse{Records: pbRecords}, nil
}

func sendCursorResult(stream grpc.BidiStreamingServer[proto.CursorClientMessage, proto.CursorResponse], entry *IndexedDBCursorEntry, indexCursor bool, err error) error {
	if err != nil {
		return providerRPCError("indexeddb cursor", err)
	}
	if entry == nil {
		return stream.Send(cursorDoneResponse(true))
	}
	pbEntry, err := cursorEntryToProto(entry, indexCursor)
	if err != nil {
		return status.Errorf(codes.Internal, "marshal cursor entry: %v", err)
	}
	return stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Entry{Entry: pbEntry}})
}

func cursorEntryToProto(entry *IndexedDBCursorEntry, indexCursor bool) (*proto.CursorEntry, error) {
	key, err := cursorKeyToProto(entry.Key, indexCursor)
	if err != nil {
		return nil, err
	}
	out := &proto.CursorEntry{Key: key, PrimaryKey: entry.PrimaryKey}
	if entry.Record != nil {
		record, err := recordToProto(entry.Record)
		if err != nil {
			return nil, err
		}
		out.Record = record
	}
	return out, nil
}

func cursorDoneResponse(done bool) *proto.CursorResponse {
	return &proto.CursorResponse{Result: &proto.CursorResponse_Done{Done: done}}
}

func cursorTargetFromProto(kvs []*proto.KeyValue, indexCursor bool) (any, error) {
	if len(kvs) == 0 {
		return nil, fmt.Errorf("continue key is required")
	}
	parts, err := keyValuesToAny(kvs)
	if err != nil {
		return nil, err
	}
	if indexCursor {
		return parts, nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return parts, nil
}

func cursorDirectionFromProto(dir proto.CursorDirection) CursorDirection {
	switch dir {
	case proto.CursorDirection_CURSOR_NEXT_UNIQUE:
		return CursorNextUnique
	case proto.CursorDirection_CURSOR_PREV:
		return CursorPrev
	case proto.CursorDirection_CURSOR_PREV_UNIQUE:
		return CursorPrevUnique
	default:
		return CursorNext
	}
}

func transactionModeFromProto(mode proto.TransactionMode) TransactionMode {
	if mode == proto.TransactionMode_TRANSACTION_READWRITE {
		return TransactionReadwrite
	}
	return TransactionReadonly
}

func durabilityHintFromProto(hint proto.TransactionDurabilityHint) TransactionDurabilityHint {
	switch hint {
	case proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_STRICT:
		return TransactionDurabilityStrict
	case proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_RELAXED:
		return TransactionDurabilityRelaxed
	default:
		return TransactionDurabilityDefault
	}
}

func executeIndexedDBOperation(ctx context.Context, tx IndexedDBTransaction, op *proto.TransactionOperation) (*proto.TransactionOperationResponse, error) {
	if op == nil {
		return nil, status.Error(codes.InvalidArgument, "transaction operation is required")
	}
	resp := &proto.TransactionOperationResponse{RequestId: op.GetRequestId()}
	switch body := op.GetOperation().(type) {
	case *proto.TransactionOperation_Get:
		record, err := tx.Get(ctx, objectStoreRequestFromProto(body.Get))
		if err != nil {
			return nil, err
		}
		pbRecord, err := recordToProto(record)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Record{Record: &proto.RecordResponse{Record: pbRecord}}
	case *proto.TransactionOperation_GetKey:
		key, err := tx.GetKey(ctx, objectStoreRequestFromProto(body.GetKey))
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Key{Key: &proto.KeyResponse{Key: key}}
	case *proto.TransactionOperation_Add:
		req, err := recordRequestFromProto(body.Add)
		if err != nil {
			return nil, err
		}
		if err := tx.Add(ctx, req); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_Put:
		req, err := recordRequestFromProto(body.Put)
		if err != nil {
			return nil, err
		}
		if err := tx.Put(ctx, req); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_Delete:
		if err := tx.Delete(ctx, objectStoreRequestFromProto(body.Delete)); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_Clear:
		if err := tx.Clear(ctx, body.Clear.GetStore()); err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: &emptypb.Empty{}}
	case *proto.TransactionOperation_GetAll:
		records, err := tx.GetAll(ctx, objectStoreRangeRequestFromProto(body.GetAll))
		if err != nil {
			return nil, err
		}
		pbRecords, err := recordsToProto(records)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Records{Records: &proto.RecordsResponse{Records: pbRecords}}
	case *proto.TransactionOperation_GetAllKeys:
		keys, err := tx.GetAllKeys(ctx, objectStoreRangeRequestFromProto(body.GetAllKeys))
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Keys{Keys: &proto.KeysResponse{Keys: keys}}
	case *proto.TransactionOperation_Count:
		count, err := tx.Count(ctx, objectStoreRangeRequestFromProto(body.Count))
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Count{Count: &proto.CountResponse{Count: count}}
	case *proto.TransactionOperation_DeleteRange:
		deleted, err := tx.DeleteRange(ctx, objectStoreRangeRequestFromProto(body.DeleteRange))
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Delete{Delete: &proto.DeleteResponse{Deleted: deleted}}
	case *proto.TransactionOperation_IndexGet:
		query, err := indexQueryRequestFromProto(body.IndexGet)
		if err != nil {
			return nil, err
		}
		record, err := tx.IndexGet(ctx, query)
		if err != nil {
			return nil, err
		}
		pbRecord, err := recordToProto(record)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Record{Record: &proto.RecordResponse{Record: pbRecord}}
	case *proto.TransactionOperation_IndexGetKey:
		query, err := indexQueryRequestFromProto(body.IndexGetKey)
		if err != nil {
			return nil, err
		}
		key, err := tx.IndexGetKey(ctx, query)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Key{Key: &proto.KeyResponse{Key: key}}
	case *proto.TransactionOperation_IndexGetAll:
		query, err := indexQueryRequestFromProto(body.IndexGetAll)
		if err != nil {
			return nil, err
		}
		records, err := tx.IndexGetAll(ctx, query)
		if err != nil {
			return nil, err
		}
		pbRecords, err := recordsToProto(records)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Records{Records: &proto.RecordsResponse{Records: pbRecords}}
	case *proto.TransactionOperation_IndexGetAllKeys:
		query, err := indexQueryRequestFromProto(body.IndexGetAllKeys)
		if err != nil {
			return nil, err
		}
		keys, err := tx.IndexGetAllKeys(ctx, query)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Keys{Keys: &proto.KeysResponse{Keys: keys}}
	case *proto.TransactionOperation_IndexCount:
		query, err := indexQueryRequestFromProto(body.IndexCount)
		if err != nil {
			return nil, err
		}
		count, err := tx.IndexCount(ctx, query)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Count{Count: &proto.CountResponse{Count: count}}
	case *proto.TransactionOperation_IndexDelete:
		query, err := indexQueryRequestFromProto(body.IndexDelete)
		if err != nil {
			return nil, err
		}
		deleted, err := tx.IndexDelete(ctx, query)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Delete{Delete: &proto.DeleteResponse{Deleted: deleted}}
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown transaction operation")
	}
	return resp, nil
}

func recordRequestFromProto(req *proto.RecordRequest) (IndexedDBRecordRequest, error) {
	record, err := recordFromProto(req.GetRecord())
	if err != nil {
		return IndexedDBRecordRequest{}, fmt.Errorf("unmarshal record: %w", err)
	}
	return IndexedDBRecordRequest{Store: req.GetStore(), Record: record}, nil
}

func transactionOperationError(requestID uint64, err error) *proto.TransactionOperationResponse {
	return &proto.TransactionOperationResponse{RequestId: requestID, Error: rpcStatusFromError(err)}
}

func rpcStatusFromError(err error) *rpcstatus.Status {
	if err == nil {
		return nil
	}
	rpcErr := providerRPCError("indexeddb", err)
	st, ok := status.FromError(rpcErr)
	if !ok {
		return &rpcstatus.Status{Code: int32(codes.Internal), Message: rpcErr.Error()}
	}
	return &rpcstatus.Status{Code: int32(st.Code()), Message: st.Message()}
}

func readonlyOperationError(mode TransactionMode, op *proto.TransactionOperation) error {
	if mode == TransactionReadwrite || op == nil {
		return nil
	}
	if isWriteTransactionOperation(op) {
		return FailedPrecondition("transaction is readonly")
	}
	return nil
}

func isWriteTransactionOperation(op *proto.TransactionOperation) bool {
	switch op.GetOperation().(type) {
	case *proto.TransactionOperation_Add,
		*proto.TransactionOperation_Put,
		*proto.TransactionOperation_Delete,
		*proto.TransactionOperation_Clear,
		*proto.TransactionOperation_DeleteRange,
		*proto.TransactionOperation_IndexDelete:
		return true
	default:
		return false
	}
}

func drainIndexedDBTransaction(stream proto.IndexedDB_TransactionServer) error {
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if strings.Contains(err.Error(), "context canceled") {
				return nil
			}
			return err
		}
	}
}
