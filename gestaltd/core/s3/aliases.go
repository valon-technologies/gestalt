package s3

import (
	"context"

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
)

type (
	ObjectRef      = s3sdk.ObjectRef
	ObjectMeta     = s3sdk.ObjectMeta
	ByteRange      = s3sdk.ByteRange
	ReadRequest    = s3sdk.ReadRequest
	ReadResult     = s3sdk.ReadResult
	WriteRequest   = s3sdk.WriteRequest
	ListRequest    = s3sdk.ListRequest
	ListPage       = s3sdk.ListPage
	CopyRequest    = s3sdk.CopyRequest
	PresignMethod  = s3sdk.PresignMethod
	PresignRequest = s3sdk.PresignRequest
	PresignResult  = s3sdk.PresignResult
)

const (
	PresignMethodGet    = s3sdk.PresignMethodGet
	PresignMethodPut    = s3sdk.PresignMethodPut
	PresignMethodDelete = s3sdk.PresignMethodDelete
	PresignMethodHead   = s3sdk.PresignMethodHead
)

var (
	ErrNotFound           = s3sdk.ErrNotFound
	ErrPreconditionFailed = s3sdk.ErrPreconditionFailed
	ErrInvalidRange       = s3sdk.ErrInvalidRange
)

type Client = s3sdk.Client

type Pinger interface {
	Ping(ctx context.Context) error
}
