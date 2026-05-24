package s3

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ClientError maps provider, transport, and AWS errors to S3 client sentinel errors.
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
	if isAWSNotFound(err) {
		return ErrNotFound
	}
	if isAWSPreconditionFailed(err) {
		return ErrPreconditionFailed
	}
	if isAWSInvalidRange(err) {
		return ErrInvalidRange
	}
	if strings.Contains(err.Error(), "expires must be") {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if strings.Contains(err.Error(), "s3: invalid range") {
		return ErrInvalidRange
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

func isAWSNotFound(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "NoSuchKey") ||
		strings.Contains(msg, "NoSuchBucket") ||
		strings.Contains(msg, "NoSuchVersion") ||
		strings.Contains(msg, "StatusCode: 404")
}

func isAWSPreconditionFailed(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "PreconditionFailed") ||
		strings.Contains(msg, "NotModified") ||
		strings.Contains(msg, "StatusCode: 412") ||
		strings.Contains(msg, "StatusCode: 304")
}

func isAWSInvalidRange(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "s3: invalid range") ||
		strings.Contains(msg, "InvalidRange") ||
		strings.Contains(msg, "StatusCode: 416")
}
