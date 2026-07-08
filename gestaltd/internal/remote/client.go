package remote

import (
	"context"
	"fmt"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Config configures an authenticated client to a remote public gestaltd API.
type Config struct {
	URL   string
	Token string
}

// ClientSet exposes typed public gRPC clients for a remote gestaltd instance.
type ClientSet struct {
	App       proto.AppClient
	Agent     proto.AgentClient
	Workflow  proto.WorkflowClient
	IndexedDB proto.IndexedDBClient

	conn *grpc.ClientConn
}

// NewClientSet dials the remote public gestaltd gRPC surface and returns typed clients.
func NewClientSet(ctx context.Context, cfg Config) (*ClientSet, error) {
	url := strings.TrimSpace(cfg.URL)
	if url == "" {
		return nil, fmt.Errorf("remote: URL is required")
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, fmt.Errorf("remote: token is required")
	}

	conn, err := dialRemote(ctx, url, token)
	if err != nil {
		return nil, err
	}
	return clientSetFromConn(conn), nil
}

// Close releases the underlying gRPC connection.
func (c *ClientSet) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	conn := c.conn
	c.conn = nil
	return conn.Close()
}

// WithBearer attaches remote bearer authorization metadata to ctx.
func WithBearer(ctx context.Context, token string) context.Context {
	token = strings.TrimSpace(token)
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

func clientSetFromConn(conn *grpc.ClientConn) *ClientSet {
	return &ClientSet{
		App:       proto.NewAppClient(conn),
		Agent:     proto.NewAgentClient(conn),
		Workflow:  proto.NewWorkflowClient(conn),
		IndexedDB: proto.NewIndexedDBClient(conn),
		conn:      conn,
	}
}
