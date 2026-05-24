package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/s3"
)

type (
	ObjectRef            = s3.ObjectRef
	ObjectMeta           = s3.ObjectMeta
	ByteRange            = s3.ByteRange
	ReadRequest          = s3.ReadRequest
	ReadResult           = s3.ReadResult
	WriteRequest         = s3.WriteRequest
	ListRequest          = s3.ListRequest
	ListPage             = s3.ListPage
	CopyRequest          = s3.CopyRequest
	PresignMethod        = s3.PresignMethod
	PresignRequest       = s3.PresignRequest
	PresignResult        = s3.PresignResult
	ReadOptions          = s3.ReadOptions
	WriteOptions         = s3.WriteOptions
	ListOptions          = s3.ListOptions
	CopyOptions          = s3.CopyOptions
	PresignOptions       = s3.PresignOptions
	ObjectAccessURLOptions = s3.ObjectAccessURLOptions
	ObjectAccessURL      = s3.ObjectAccessURL
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
)

type S3Client = s3.HostClient
type Object = s3.Object

// S3 connects to the S3 provider exposed by gestaltd.
func S3(ctx context.Context, name ...string) (s3.Client, error) {
	opts := s3.OpenOptions{}
	if len(name) > 0 {
		opts.Binding = name[0]
	}
	return s3.Open(ctx, opts)
}
