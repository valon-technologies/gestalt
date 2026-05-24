package indexeddb

import (
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RPCError maps provider/host gRPC errors to indexeddb sentinel errors.
func RPCError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return ErrNotFound
	case codes.AlreadyExists:
		return ErrAlreadyExists
	case codes.InvalidArgument:
		if strings.Contains(st.Message(), "invalid transaction") {
			return ErrInvalidTransaction
		}
		return err
	case codes.FailedPrecondition:
		if strings.Contains(st.Message(), "readonly") {
			return ErrReadOnly
		}
		if strings.Contains(st.Message(), "already finished") {
			return ErrTransactionDone
		}
		return err
	default:
		return err
	}
}
