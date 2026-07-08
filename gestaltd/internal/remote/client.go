package remote

import (
	"context"
	"fmt"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Config configures an authenticated client set for a remote public gestaltd.
type Config struct {
	URL   string
	Token string
}

// ClientSet exposes generated public gRPC clients for a remote gestaltd.
type ClientSet struct {
	App       proto.AppClient
	Agent     proto.AgentClient
	Workflow  proto.WorkflowClient
	IndexedDB proto.IndexedDBClient
	Close     func() error
}

// NewClientSet dials the remote gestaltd public gRPC surface and returns typed clients.
func NewClientSet(ctx context.Context, cfg Config) (*ClientSet, error) {
	url := strings.TrimSpace(cfg.URL)
	if url == "" {
		return nil, fmt.Errorf("remote URL is required")
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, fmt.Errorf("remote token is required")
	}

	target, dialOpts, err := dialOptions(url)
	if err != nil {
		return nil, err
	}
	dialOpts = append(dialOpts,
		grpc.WithUnaryInterceptor(bearerUnaryInterceptor(token)),
		grpc.WithStreamInterceptor(bearerStreamInterceptor(token)),
	)

	conn, err := grpc.NewClient(target, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial remote gestaltd %q: %w", url, err)
	}
	if ctx != nil {
		if err := waitForReady(ctx, conn); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	return &ClientSet{
		App:       proto.NewAppClient(conn),
		Agent:     proto.NewAgentClient(conn),
		Workflow:  proto.NewWorkflowClient(conn),
		IndexedDB: proto.NewIndexedDBClient(conn),
		Close:     conn.Close,
	}, nil
}

// WithBearer attaches authorization metadata for remote public gRPC calls.
func WithBearer(ctx context.Context, token string) context.Context {
	token = strings.TrimSpace(token)
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

func bearerUnaryInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(WithBearer(ctx, token), method, req, reply, cc, opts...)
	}
}

func bearerStreamInterceptor(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(WithBearer(ctx, token), desc, cc, method, opts...)
	}
}
