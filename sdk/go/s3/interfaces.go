package s3

import (
	"context"
	"io"
)

// S3 is the request-oriented S3-compatible capability interface.
type S3 interface {
	HeadObject(ctx context.Context, ref ObjectRef) (ObjectMeta, error)
	ReadObject(ctx context.Context, req ReadRequest) (ReadResult, error)
	WriteObject(ctx context.Context, req WriteRequest) (ObjectMeta, error)
	DeleteObject(ctx context.Context, ref ObjectRef) error
	ListObjects(ctx context.Context, req ListRequest) (ListPage, error)
	CopyObject(ctx context.Context, req CopyRequest) (ObjectMeta, error)
	PresignObject(ctx context.Context, req PresignRequest) (PresignResult, error)
	Close() error
}

// Pinger is a server-side health extension.
type Pinger interface {
	Ping(ctx context.Context) error
}

// ObjectHandle provides optional object-key convenience helpers.
type ObjectHandle interface {
	Stat(ctx context.Context) (ObjectMeta, error)
	Exists(ctx context.Context) (bool, error)
	Stream(ctx context.Context, req *ReadRequest) (ObjectMeta, io.ReadCloser, error)
	Bytes(ctx context.Context, req *ReadRequest) ([]byte, error)
	Text(ctx context.Context, req *ReadRequest) (string, error)
	JSON(ctx context.Context, req *ReadRequest) (any, error)
	Write(ctx context.Context, req WriteRequest) (ObjectMeta, error)
	WriteBytes(ctx context.Context, body []byte, req *WriteRequest) (ObjectMeta, error)
	WriteString(ctx context.Context, body string, req *WriteRequest) (ObjectMeta, error)
	WriteJSON(ctx context.Context, value any, req *WriteRequest) (ObjectMeta, error)
	Delete(ctx context.Context) error
	Presign(ctx context.Context, req *PresignRequest) (PresignResult, error)
}
