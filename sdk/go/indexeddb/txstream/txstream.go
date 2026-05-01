package txstream

import (
	"context"
	"errors"
	"io"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Backend interface {
	Get(context.Context, *proto.ObjectStoreRequest) (*proto.RecordResponse, error)
	GetKey(context.Context, *proto.ObjectStoreRequest) (*proto.KeyResponse, error)
	Add(context.Context, *proto.RecordRequest) (*emptypb.Empty, error)
	Put(context.Context, *proto.RecordRequest) (*emptypb.Empty, error)
	Delete(context.Context, *proto.ObjectStoreRequest) (*emptypb.Empty, error)
	Clear(context.Context, *proto.ObjectStoreNameRequest) (*emptypb.Empty, error)
	GetAll(context.Context, *proto.ObjectStoreRangeRequest) (*proto.RecordsResponse, error)
	GetAllKeys(context.Context, *proto.ObjectStoreRangeRequest) (*proto.KeysResponse, error)
	Count(context.Context, *proto.ObjectStoreRangeRequest) (*proto.CountResponse, error)
	DeleteRange(context.Context, *proto.ObjectStoreRangeRequest) (*proto.DeleteResponse, error)
	IndexGet(context.Context, *proto.IndexQueryRequest) (*proto.RecordResponse, error)
	IndexGetKey(context.Context, *proto.IndexQueryRequest) (*proto.KeyResponse, error)
	IndexGetAll(context.Context, *proto.IndexQueryRequest) (*proto.RecordsResponse, error)
	IndexGetAllKeys(context.Context, *proto.IndexQueryRequest) (*proto.KeysResponse, error)
	IndexCount(context.Context, *proto.IndexQueryRequest) (*proto.CountResponse, error)
	IndexDelete(context.Context, *proto.IndexQueryRequest) (*proto.DeleteResponse, error)
}

type Transaction interface {
	Backend
	Commit(context.Context) error
	Abort(context.Context) error
}

type BeginFunc func(context.Context, *proto.BeginTransactionRequest) (Transaction, error)

func Serve(stream proto.IndexedDB_TransactionServer, begin BeginFunc) error {
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

	tx, err := begin(stream.Context(), beginReq)
	if err != nil {
		return err
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
			opErr := readonlyOperationError(beginReq.GetMode(), body.Operation)
			resp := (*proto.TransactionOperationResponse)(nil)
			if opErr == nil {
				resp, opErr = ExecuteOperation(stream.Context(), tx, body.Operation)
			}
			if opErr != nil {
				finished = true
				abortErr := tx.Abort(stream.Context())
				if err := stream.Send(&proto.TransactionServerMessage{
					Msg: &proto.TransactionServerMessage_Operation{Operation: OperationError(body.Operation.GetRequestId(), opErr)},
				}); err != nil {
					return err
				}
				if err := stream.Send(&proto.TransactionServerMessage{
					Msg: &proto.TransactionServerMessage_Abort{Abort: &proto.TransactionAbortResponse{Error: RPCStatusFromError(abortErr)}},
				}); err != nil {
					return err
				}
				return drain(stream)
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
				Msg: &proto.TransactionServerMessage_Commit{Commit: &proto.TransactionCommitResponse{Error: RPCStatusFromError(commitErr)}},
			})
		case *proto.TransactionClientMessage_Abort:
			finished = true
			abortErr := tx.Abort(stream.Context())
			return stream.Send(&proto.TransactionServerMessage{
				Msg: &proto.TransactionServerMessage_Abort{Abort: &proto.TransactionAbortResponse{Error: RPCStatusFromError(abortErr)}},
			})
		default:
			finished = true
			_ = tx.Abort(stream.Context())
			return status.Error(codes.InvalidArgument, "expected transaction operation, commit, or abort")
		}
	}
}

func ExecuteOperation(ctx context.Context, backend Backend, op *proto.TransactionOperation) (*proto.TransactionOperationResponse, error) {
	if op == nil {
		return nil, status.Error(codes.InvalidArgument, "transaction operation is required")
	}
	resp := &proto.TransactionOperationResponse{RequestId: op.GetRequestId()}
	switch body := op.GetOperation().(type) {
	case *proto.TransactionOperation_Get:
		record, err := backend.Get(ctx, body.Get)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Record{Record: record}
	case *proto.TransactionOperation_GetKey:
		key, err := backend.GetKey(ctx, body.GetKey)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Key{Key: key}
	case *proto.TransactionOperation_Add:
		empty, err := backend.Add(ctx, body.Add)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: empty}
	case *proto.TransactionOperation_Put:
		empty, err := backend.Put(ctx, body.Put)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: empty}
	case *proto.TransactionOperation_Delete:
		empty, err := backend.Delete(ctx, body.Delete)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: empty}
	case *proto.TransactionOperation_Clear:
		empty, err := backend.Clear(ctx, body.Clear)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Empty{Empty: empty}
	case *proto.TransactionOperation_GetAll:
		records, err := backend.GetAll(ctx, body.GetAll)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Records{Records: records}
	case *proto.TransactionOperation_GetAllKeys:
		keys, err := backend.GetAllKeys(ctx, body.GetAllKeys)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Keys{Keys: keys}
	case *proto.TransactionOperation_Count:
		count, err := backend.Count(ctx, body.Count)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Count{Count: count}
	case *proto.TransactionOperation_DeleteRange:
		deleted, err := backend.DeleteRange(ctx, body.DeleteRange)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Delete{Delete: deleted}
	case *proto.TransactionOperation_IndexGet:
		record, err := backend.IndexGet(ctx, body.IndexGet)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Record{Record: record}
	case *proto.TransactionOperation_IndexGetKey:
		key, err := backend.IndexGetKey(ctx, body.IndexGetKey)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Key{Key: key}
	case *proto.TransactionOperation_IndexGetAll:
		records, err := backend.IndexGetAll(ctx, body.IndexGetAll)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Records{Records: records}
	case *proto.TransactionOperation_IndexGetAllKeys:
		keys, err := backend.IndexGetAllKeys(ctx, body.IndexGetAllKeys)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Keys{Keys: keys}
	case *proto.TransactionOperation_IndexCount:
		count, err := backend.IndexCount(ctx, body.IndexCount)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Count{Count: count}
	case *proto.TransactionOperation_IndexDelete:
		deleted, err := backend.IndexDelete(ctx, body.IndexDelete)
		if err != nil {
			return nil, err
		}
		resp.Result = &proto.TransactionOperationResponse_Delete{Delete: deleted}
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown transaction operation")
	}
	return resp, nil
}

func OperationError(requestID uint64, err error) *proto.TransactionOperationResponse {
	return &proto.TransactionOperationResponse{
		RequestId: requestID,
		Error:     RPCStatusFromError(err),
	}
}

func RPCStatusFromError(err error) *rpcstatus.Status {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return &rpcstatus.Status{Code: int32(codes.Internal), Message: err.Error()}
	}
	return &rpcstatus.Status{Code: int32(st.Code()), Message: st.Message()}
}

func readonlyOperationError(mode proto.TransactionMode, op *proto.TransactionOperation) error {
	if mode == proto.TransactionMode_TRANSACTION_READWRITE || op == nil {
		return nil
	}
	if isWriteOperation(op) {
		return status.Error(codes.FailedPrecondition, "transaction is readonly")
	}
	return nil
}

func isWriteOperation(op *proto.TransactionOperation) bool {
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

func drain(stream proto.IndexedDB_TransactionServer) error {
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
