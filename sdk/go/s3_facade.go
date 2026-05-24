package gestalt

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	"github.com/valon-technologies/gestalt/sdk/go/s3"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	rpcs3 "github.com/valon-technologies/gestalt/server/rpc/s3"
	"google.golang.org/grpc"
)

type (
	ObjectRef              = s3.ObjectRef
	ObjectMeta             = s3.ObjectMeta
	ByteRange              = s3.ByteRange
	ReadRequest            = s3.ReadRequest
	ReadResult             = s3.ReadResult
	WriteRequest           = s3.WriteRequest
	ListRequest            = s3.ListRequest
	ListPage               = s3.ListPage
	CopyRequest            = s3.CopyRequest
	PresignMethod          = s3.PresignMethod
	PresignRequest         = s3.PresignRequest
	PresignResult          = s3.PresignResult
	ReadOptions            = s3.ReadOptions
	WriteOptions           = s3.WriteOptions
	ListOptions            = s3.ListOptions
	CopyOptions            = s3.CopyOptions
	PresignOptions         = s3.PresignOptions
	ObjectAccessURLOptions = s3.ObjectAccessURLOptions
	ObjectAccessURL        = s3.ObjectAccessURL
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
	ErrS3Unsupported        = s3.ErrUnsupported
)

type Object = s3.ObjectHandleRef

// MapProviderClientError maps provider and transport errors to S3 client sentinel errors.
func MapProviderClientError(err error) error {
	if err == nil {
		return nil
	}
	if code, ok := StatusCodeOf(err); ok {
		switch code {
		case CodeNotFound:
			return s3.ErrNotFound
		case CodeFailedPrecondition:
			return s3.ErrPreconditionFailed
		case CodeOutOfRange:
			return s3.ErrInvalidRange
		}
	}
	return s3.ClientError(err)
}

type s3SharedTransport struct {
	mu                 sync.Mutex
	target             string
	token              string
	binding            string
	conn               *grpc.ClientConn
	client             proto.S3Client
	objectAccessClient proto.S3ObjectAccessClient
}

var s3Transports sync.Map

// S3 connects to the S3 provider exposed by gestaltd.
func S3(ctx context.Context, name ...string) (s3.S3, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target, token, err := host.Target("s3")
	if err != nil {
		return nil, err
	}
	binding := ""
	if len(name) > 0 {
		binding = strings.TrimSpace(name[0])
	}
	transport := getS3Transport(binding)
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	grpcClient, objectAccess, err := sharedS3Clients(dialCtx, target, token, binding, transport)
	if err != nil {
		return nil, fmt.Errorf("s3: connect to host: %w", err)
	}
	return rpcs3.NewClient(grpcClient, objectAccess, rpcs3.Options{}), nil
}

func getS3Transport(binding string) *s3SharedTransport {
	val, _ := s3Transports.LoadOrStore(binding, &s3SharedTransport{})
	return val.(*s3SharedTransport)
}

func sharedS3Clients(ctx context.Context, target, token, binding string, transport *s3SharedTransport) (proto.S3Client, proto.S3ObjectAccessClient, error) {
	if transport == nil {
		return nil, nil, fmt.Errorf("s3: shared transport is not initialized")
	}
	transport.mu.Lock()
	if transport.conn != nil && transport.target == target && transport.token == token && transport.binding == binding {
		client := transport.client
		objectAccessClient := transport.objectAccessClient
		transport.mu.Unlock()
		return client, objectAccessClient, nil
	}
	transport.mu.Unlock()

	conn, err := host.DialService(ctx, "s3", target, token, binding)
	if err != nil {
		return nil, nil, err
	}
	client := proto.NewS3Client(conn)
	objectAccessClient := proto.NewS3ObjectAccessClient(conn)

	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.conn != nil && transport.target == target && transport.token == token && transport.binding == binding {
		_ = conn.Close()
		return transport.client, transport.objectAccessClient, nil
	}
	if transport.conn != nil {
		_ = transport.conn.Close()
	}
	transport.target = target
	transport.token = token
	transport.binding = binding
	transport.conn = conn
	transport.client = client
	transport.objectAccessClient = objectAccessClient
	return client, objectAccessClient, nil
}
