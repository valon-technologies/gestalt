package s3

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ClientError maps provider and transport gRPC errors to S3 client sentinel errors.
func ClientError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrPreconditionFailed) || errors.Is(err, ErrInvalidRange) {
		return err
	}
	if mapped := rpcClientError(err); mapped != err {
		return mapped
	}
	if strings.Contains(err.Error(), "expires must be") {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return err
}

func rpcClientError(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return ErrNotFound
	case codes.FailedPrecondition:
		return ErrPreconditionFailed
	case codes.OutOfRange:
		return ErrInvalidRange
	default:
		return err
	}
}
