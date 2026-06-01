package gestalt

import (
	"context"
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
