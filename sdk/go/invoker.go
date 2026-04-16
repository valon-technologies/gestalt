package gestalt

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

const EnvPluginInvokerSocket = proto.EnvPluginInvokerSocket

type InvokeOptions struct {
	Connection string
	Instance   string
}

type InvokerClient struct {
	client        proto.PluginInvokerClient
	requestHandle string
}

var sharedInvokerTransport struct {
	mu         sync.Mutex
	socketPath string
	conn       *grpc.ClientConn
	client     proto.PluginInvokerClient
}

func Invoker(requestHandle string) (*InvokerClient, error) {
	if requestHandle == "" {
		return nil, fmt.Errorf("plugin invoker: request handle is not available")
	}
	socketPath := os.Getenv(EnvPluginInvokerSocket)
	if socketPath == "" {
		return nil, fmt.Errorf("plugin invoker: %s is not set", EnvPluginInvokerSocket)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := sharedPluginInvokerClient(ctx, socketPath)
	if err != nil {
		return nil, err
	}

	return &InvokerClient{
		client:        client,
		requestHandle: requestHandle,
	}, nil
}

func sharedPluginInvokerClient(ctx context.Context, socketPath string) (proto.PluginInvokerClient, error) {
	sharedInvokerTransport.mu.Lock()
	if sharedInvokerTransport.conn != nil && sharedInvokerTransport.socketPath == socketPath {
		client := sharedInvokerTransport.client
		sharedInvokerTransport.mu.Unlock()
		return client, nil
	}
	sharedInvokerTransport.mu.Unlock()

	conn, err := grpc.DialContext(ctx, "unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("plugin invoker: connect to host: %w", err)
	}

	client := proto.NewPluginInvokerClient(conn)

	sharedInvokerTransport.mu.Lock()
	defer sharedInvokerTransport.mu.Unlock()

	if sharedInvokerTransport.conn != nil && sharedInvokerTransport.socketPath == socketPath {
		_ = conn.Close()
		return sharedInvokerTransport.client, nil
	}
	if sharedInvokerTransport.conn != nil {
		_ = sharedInvokerTransport.conn.Close()
	}

	sharedInvokerTransport.socketPath = socketPath
	sharedInvokerTransport.conn = conn
	sharedInvokerTransport.client = client
	return client, nil
}

func InvokerFromContext(ctx context.Context) (*InvokerClient, error) {
	return Invoker(RequestHandleFromContext(ctx))
}

func (c *InvokerClient) Close() error {
	return nil
}

func (c *InvokerClient) Invoke(ctx context.Context, plugin, operation string, params map[string]any, opts *InvokeOptions) (*OperationResult, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("plugin invoker: client is not initialized")
	}
	if params == nil {
		params = map[string]any{}
	}
	msg, err := structpb.NewStruct(params)
	if err != nil {
		return nil, fmt.Errorf("plugin invoker: encode params: %w", err)
	}

	req := &proto.PluginInvokeRequest{
		RequestHandle: c.requestHandle,
		Plugin:        plugin,
		Operation:     operation,
		Params:        msg,
	}
	if opts != nil {
		req.Connection = opts.Connection
		req.Instance = opts.Instance
	}

	resp, err := c.client.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	return &OperationResult{
		Status: int(resp.GetStatus()),
		Body:   resp.GetBody(),
	}, nil
}
