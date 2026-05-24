package hosts3

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	"github.com/valon-technologies/gestalt/sdk/go/s3"
	"google.golang.org/grpc"
)

// OpenOptions configures s3.Open.
type OpenOptions struct {
	Binding string
}

var sharedTransports sync.Map

type sharedTransport struct {
	mu                 sync.Mutex
	target             string
	token              string
	binding            string
	conn               *grpc.ClientConn
	client             proto.S3Client
	objectAccessClient proto.S3ObjectAccessClient
}

// Open connects to the S3 provider exposed by gestaltd.
func Open(ctx context.Context, opts OpenOptions) (s3.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target, token, err := host.Target("s3")
	if err != nil {
		return nil, err
	}
	binding := strings.TrimSpace(opts.Binding)
	transport := getSharedTransport(binding)
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, objectAccess, err := sharedClients(dialCtx, target, token, binding, transport)
	if err != nil {
		return nil, fmt.Errorf("s3: connect to host: %w", err)
	}
	return &HostClient{client: client, objectAccessClient: objectAccess}, nil
}

func getSharedTransport(binding string) *sharedTransport {
	val, _ := sharedTransports.LoadOrStore(binding, &sharedTransport{})
	return val.(*sharedTransport)
}

func sharedClients(ctx context.Context, target, token, binding string, transport *sharedTransport) (proto.S3Client, proto.S3ObjectAccessClient, error) {
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
