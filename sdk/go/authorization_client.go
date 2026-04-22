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
	"google.golang.org/protobuf/types/known/emptypb"
)

const EnvAuthorizationSocket = "GESTALT_AUTHORIZATION_SOCKET"

type AuthorizationClient struct {
	client proto.AuthorizationProviderClient
}

var sharedAuthorizationTransport struct {
	mu         sync.Mutex
	socketPath string
	conn       *grpc.ClientConn
	client     proto.AuthorizationProviderClient
}

func Authorization() (*AuthorizationClient, error) {
	socketPath := os.Getenv(EnvAuthorizationSocket)
	if socketPath == "" {
		return nil, fmt.Errorf("authorization: %s is not set", EnvAuthorizationSocket)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := sharedAuthorizationClient(ctx, socketPath)
	if err != nil {
		return nil, err
	}
	return &AuthorizationClient{client: client}, nil
}

func sharedAuthorizationClient(ctx context.Context, socketPath string) (proto.AuthorizationProviderClient, error) {
	sharedAuthorizationTransport.mu.Lock()
	if sharedAuthorizationTransport.conn != nil && sharedAuthorizationTransport.socketPath == socketPath {
		client := sharedAuthorizationTransport.client
		sharedAuthorizationTransport.mu.Unlock()
		return client, nil
	}
	sharedAuthorizationTransport.mu.Unlock()

	conn, err := grpc.DialContext(ctx, "unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("authorization: connect to host: %w", err)
	}

	client := proto.NewAuthorizationProviderClient(conn)

	sharedAuthorizationTransport.mu.Lock()
	defer sharedAuthorizationTransport.mu.Unlock()

	if sharedAuthorizationTransport.conn != nil && sharedAuthorizationTransport.socketPath == socketPath {
		_ = conn.Close()
		return sharedAuthorizationTransport.client, nil
	}
	if sharedAuthorizationTransport.conn != nil {
		_ = sharedAuthorizationTransport.conn.Close()
	}

	sharedAuthorizationTransport.socketPath = socketPath
	sharedAuthorizationTransport.conn = conn
	sharedAuthorizationTransport.client = client
	return client, nil
}

func (c *AuthorizationClient) Close() error { return nil }

func (c *AuthorizationClient) SearchSubjects(ctx context.Context, req *SubjectSearchRequest) (*SubjectSearchResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	return c.client.SearchSubjects(ctx, req)
}

func (c *AuthorizationClient) GetMetadata(ctx context.Context) (*AuthorizationMetadata, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	return c.client.GetMetadata(ctx, &emptypb.Empty{})
}
