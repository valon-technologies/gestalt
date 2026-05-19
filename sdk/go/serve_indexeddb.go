package gestalt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

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

func sendCursorResult(stream grpc.BidiStreamingServer[proto.CursorClientMessage, proto.CursorResponse], entry *IDBCursorEntry, indexCursor bool, err error) error {
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

func cursorEntryToProto(entry *IDBCursorEntry, indexCursor bool) (*proto.CursorEntry, error) {
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

func executeIndexedDBOperation(ctx context.Context, tx IDBTransaction, op *proto.TransactionOperation) (*proto.TransactionOperationResponse, error) {
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
