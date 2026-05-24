package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/internal/hosts3"
	"github.com/valon-technologies/gestalt/sdk/go/s3"
)

type (
	ObjectRef              = s3.ObjectRef
	ObjectMeta             = s3.ObjectMeta
	ByteRange              = s3.ByteRange
	ReadRequest            = s3.ReadRequest
	ReadResult             = s3.ReadResult
	WriteRequest           = s3.WriteRequest
	ListRequest            = s3.ListRequest
	ListPage               = s3.ListPage
	CopyRequest            = s3.CopyRequest
	PresignMethod          = s3.PresignMethod
	PresignRequest         = s3.PresignRequest
	PresignResult          = s3.PresignResult
	ReadOptions            = s3.ReadOptions
	WriteOptions           = s3.WriteOptions
	ListOptions            = s3.ListOptions
	CopyOptions            = s3.CopyOptions
	PresignOptions         = s3.PresignOptions
	ObjectAccessURLOptions = s3.ObjectAccessURLOptions
	ObjectAccessURL        = s3.ObjectAccessURL
)

const (
	PresignMethodGet    = s3.PresignMethodGet
	PresignMethodPut    = s3.PresignMethodPut
	PresignMethodDelete = s3.PresignMethodDelete
	PresignMethodHead   = s3.PresignMethodHead
)

var (
	ErrS3NotFound           = s3.ErrNotFound
	ErrS3PreconditionFailed = s3.ErrPreconditionFailed
	ErrS3InvalidRange       = s3.ErrInvalidRange
	ErrS3Unsupported        = s3.ErrUnsupported
)

type (
	S3Client = s3.Client
	Object   = s3.ObjectHandleRef
)

// MapProviderClientError maps provider and transport errors to S3 client sentinel errors.
func MapProviderClientError(err error) error {
	if err == nil {
		return nil
	}
	if code, ok := StatusCodeOf(err); ok {
		switch code {
		case CodeNotFound:
			return s3.ErrNotFound
		case CodeFailedPrecondition:
			return s3.ErrPreconditionFailed
		case CodeOutOfRange:
			return s3.ErrInvalidRange
		}
	}
	return s3.ClientError(err)
}

// S3 connects to the S3 provider exposed by gestaltd.
func S3(ctx context.Context, name ...string) (s3.Client, error) {
	opts := hosts3.OpenOptions{}
	if len(name) > 0 {
		opts.Binding = name[0]
	}
	return hosts3.Open(ctx, opts)
}
