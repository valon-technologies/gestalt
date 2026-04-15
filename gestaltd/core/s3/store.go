package s3

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrNotFound           = errors.New("s3: not found")
	ErrPreconditionFailed = errors.New("s3: precondition failed")
	ErrInvalidRange       = errors.New("s3: invalid range")
)

type ObjectRef struct {
	Bucket    string
	Key       string
	VersionID string
}

type ObjectMeta struct {
	Ref          ObjectRef
	ETag         string
	Size         int64
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
	StorageClass string
}

type ByteRange struct {
	Start *int64
	End   *int64
}

type ReadRequest struct {
	Ref               ObjectRef
	Range             *ByteRange
	IfMatch           string
	IfNoneMatch       string
	IfModifiedSince   *time.Time
	IfUnmodifiedSince *time.Time
}

type ReadResult struct {
	Meta ObjectMeta
	Body io.ReadCloser
}

type WriteRequest struct {
	Ref                ObjectRef
	ContentType        string
	CacheControl       string
	ContentDisposition string
	ContentEncoding    string
	ContentLanguage    string
	Metadata           map[string]string
	IfMatch            string
	IfNoneMatch        string
	Body               io.Reader
}

type ListRequest struct {
	Bucket            string
	Prefix            string
	Delimiter         string
	ContinuationToken string
	StartAfter        string
	MaxKeys           int32
}

type ListPage struct {
	Objects               []ObjectMeta
	CommonPrefixes        []string
	NextContinuationToken string
	HasMore               bool
}

type CopyRequest struct {
	Source      ObjectRef
	Destination ObjectRef
	IfMatch     string
	IfNoneMatch string
}

type PresignMethod string

const (
	PresignMethodGet    PresignMethod = "GET"
	PresignMethodPut    PresignMethod = "PUT"
	PresignMethodDelete PresignMethod = "DELETE"
	PresignMethodHead   PresignMethod = "HEAD"
)

type PresignRequest struct {
	Ref                ObjectRef
	Method             PresignMethod
	Expires            time.Duration
	ContentType        string
	ContentDisposition string
	Headers            map[string]string
}

type PresignResult struct {
	URL       string
	Method    PresignMethod
	ExpiresAt time.Time
	Headers   map[string]string
}

type Client interface {
	HeadObject(ctx context.Context, ref ObjectRef) (ObjectMeta, error)
	ReadObject(ctx context.Context, req ReadRequest) (ReadResult, error)
	WriteObject(ctx context.Context, req WriteRequest) (ObjectMeta, error)
	DeleteObject(ctx context.Context, ref ObjectRef) error
	ListObjects(ctx context.Context, req ListRequest) (ListPage, error)
	CopyObject(ctx context.Context, req CopyRequest) (ObjectMeta, error)
	PresignObject(ctx context.Context, req PresignRequest) (PresignResult, error)
	Ping(ctx context.Context) error
	Close() error
}
