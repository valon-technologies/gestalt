package s3

import (
	"context"
	"io"
)

// Client is the request-oriented S3 capability interface.
type Client interface {
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
	Stream(ctx context.Context, opts *ReadOptions) (ObjectMeta, io.ReadCloser, error)
	Bytes(ctx context.Context, opts *ReadOptions) ([]byte, error)
	Text(ctx context.Context, opts *ReadOptions) (string, error)
	JSON(ctx context.Context, opts *ReadOptions) (any, error)
	Write(ctx context.Context, body io.Reader, opts *WriteOptions) (ObjectMeta, error)
	WriteBytes(ctx context.Context, body []byte, opts *WriteOptions) (ObjectMeta, error)
	WriteString(ctx context.Context, body string, opts *WriteOptions) (ObjectMeta, error)
	WriteJSON(ctx context.Context, value any, opts *WriteOptions) (ObjectMeta, error)
	Delete(ctx context.Context) error
	Presign(ctx context.Context, opts *PresignOptions) (PresignResult, error)
}
