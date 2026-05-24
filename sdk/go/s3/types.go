package s3

import (
	"io"
	"time"
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

// ReadOptions is the legacy convenience read shape used by Object helpers.
type ReadOptions struct {
	Range             *ByteRange
	IfMatch           string
	IfNoneMatch       string
	IfModifiedSince   *time.Time
	IfUnmodifiedSince *time.Time
}

// WriteOptions is the legacy convenience write shape used by Object helpers.
type WriteOptions struct {
	ContentType        string
	CacheControl       string
	ContentDisposition string
	ContentEncoding    string
	ContentLanguage    string
	Metadata           map[string]string
	IfMatch            string
	IfNoneMatch        string
}

// CopyOptions is the legacy convenience copy shape.
type CopyOptions = CopyRequest

// ListOptions is the legacy convenience list shape.
type ListOptions = ListRequest

// PresignOptions is the legacy convenience presign shape.
type PresignOptions struct {
	Method             PresignMethod
	Expires            time.Duration
	ContentType        string
	ContentDisposition string
	Headers            map[string]string
}

type ObjectAccessURLOptions = PresignOptions
type ObjectAccessURL = PresignResult
