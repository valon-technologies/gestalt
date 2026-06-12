package s3

import (
	"io"
	"time"
)

// ObjectRef identifies an object by key and optional version.
type ObjectRef struct {
	Key       string
	VersionID string
}

// ObjectMeta describes a stored object without its body.
type ObjectMeta struct {
	Ref          ObjectRef
	ETag         string
	Size         int64
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
	StorageClass string
}

// ByteRange requests an inclusive slice of an object's bytes; nil bounds
// are open ended.
type ByteRange struct {
	Start *int64
	End   *int64
}

// ReadRequest asks a backend for an object body with optional range and
// conditional headers.
type ReadRequest struct {
	Ref               ObjectRef
	Range             *ByteRange
	IfMatch           string
	IfNoneMatch       string
	IfModifiedSince   *time.Time
	IfUnmodifiedSince *time.Time
}

// ReadResult carries a read object's metadata and its body stream; the
// caller owns closing the body.
type ReadResult struct {
	Meta ObjectMeta
	Body io.ReadCloser
}

// WriteRequest stores an object body with its content headers, user
// metadata, and conditional headers.
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

// ListRequest pages through object listings under a prefix.
type ListRequest struct {
	Prefix            string
	Delimiter         string
	ContinuationToken string
	StartAfter        string
	MaxKeys           int32
}

// ListPage is one page of a listing with its continuation state.
type ListPage struct {
	Objects               []ObjectMeta
	CommonPrefixes        []string
	NextContinuationToken string
	HasMore               bool
}

// CopyRequest copies one object to another location with optional
// conditional headers on the source.
type CopyRequest struct {
	Source      ObjectRef
	Destination ObjectRef
	IfMatch     string
	IfNoneMatch string
}

// PresignMethod is the HTTP verb a presigned URL grants.
type PresignMethod string

// The presignable HTTP verbs.
const (
	PresignMethodGet    PresignMethod = "GET"
	PresignMethodPut    PresignMethod = "PUT"
	PresignMethodDelete PresignMethod = "DELETE"
	PresignMethodHead   PresignMethod = "HEAD"
)

// PresignRequest asks a backend to sign a URL for one object operation.
type PresignRequest struct {
	Ref                ObjectRef
	Method             PresignMethod
	Expires            time.Duration
	ContentType        string
	ContentDisposition string
	Headers            map[string]string
}

// PresignResult is the signed URL with the verb, expiry, and headers the
// caller must send with it.
type PresignResult struct {
	URL       string
	Method    PresignMethod
	ExpiresAt time.Time
	Headers   map[string]string
}
