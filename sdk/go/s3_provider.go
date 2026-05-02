package gestalt

import (
	"context"
	"io"
)

// S3Provider is implemented by providers that serve an S3-compatible
// object-store surface over gRPC.
type S3Provider interface {
	Provider
	HeadObject(ctx context.Context, ref ObjectRef) (ObjectMeta, error)
	ReadObject(ctx context.Context, ref ObjectRef, opts *ReadOptions) (ObjectMeta, io.ReadCloser, error)
	WriteObject(ctx context.Context, ref ObjectRef, body io.Reader, opts *WriteOptions) (ObjectMeta, error)
	DeleteObject(ctx context.Context, ref ObjectRef) error
	ListObjects(ctx context.Context, opts ListOptions) (ListPage, error)
	CopyObject(ctx context.Context, source ObjectRef, destination ObjectRef, opts *CopyOptions) (ObjectMeta, error)
	PresignObject(ctx context.Context, ref ObjectRef, opts *PresignOptions) (PresignResult, error)
}
