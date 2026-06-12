package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/s3"
)

// Aliases to the native S3 types in sdk/go/s3, shared by the S3 provider
// surface.
//
//nolint:revive // grouped aliases documented at their canonical definitions
type (
	ObjectRef      = s3.ObjectRef
	ObjectMeta     = s3.ObjectMeta
	ByteRange      = s3.ByteRange
	ReadRequest    = s3.ReadRequest
	ReadResult     = s3.ReadResult
	WriteRequest   = s3.WriteRequest
	ListRequest    = s3.ListRequest
	ListPage       = s3.ListPage
	CopyRequest    = s3.CopyRequest
	PresignMethod  = s3.PresignMethod
	PresignRequest = s3.PresignRequest
	PresignResult  = s3.PresignResult
)

// S3Provider is implemented by providers that serve an S3-compatible
// object-store surface over gRPC.
type S3Provider interface {
	Provider
	HeadObject(ctx context.Context, ref ObjectRef) (ObjectMeta, error)
	ReadObject(ctx context.Context, req ReadRequest) (ReadResult, error)
	WriteObject(ctx context.Context, req WriteRequest) (ObjectMeta, error)
	DeleteObject(ctx context.Context, ref ObjectRef) error
	ListObjects(ctx context.Context, req ListRequest) (ListPage, error)
	CopyObject(ctx context.Context, req CopyRequest) (ObjectMeta, error)
	PresignObject(ctx context.Context, req PresignRequest) (PresignResult, error)
}
