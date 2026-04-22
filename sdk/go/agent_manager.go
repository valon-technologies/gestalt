package gestalt

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	gproto "google.golang.org/protobuf/proto"
)

const EnvAgentManagerSocket = proto.EnvAgentManagerSocket
const EnvAgentManagerSocketToken = EnvAgentManagerSocket + "_TOKEN"

type AgentManagerClient struct {
	client          proto.AgentManagerHostClient
	invocationToken string
}

var sharedAgentManagerTransport struct {
	mu     sync.Mutex
	target string
	token  string
	conn   *grpc.ClientConn
	client proto.AgentManagerHostClient
}

func AgentManager(invocationToken string) (*AgentManagerClient, error) {
	if strings.TrimSpace(invocationToken) == "" {
		return nil, fmt.Errorf("agent manager: invocation token is not available")
	}
	target := os.Getenv(EnvAgentManagerSocket)
	if target == "" {
		return nil, fmt.Errorf("agent manager: %s is not set", EnvAgentManagerSocket)
	}
	token := os.Getenv(EnvAgentManagerSocketToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := sharedAgentManagerClient(ctx, target, token)
	if err != nil {
		return nil, err
	}

	return &AgentManagerClient{client: client, invocationToken: strings.TrimSpace(invocationToken)}, nil
}

func sharedAgentManagerClient(ctx context.Context, target, token string) (proto.AgentManagerHostClient, error) {
	sharedAgentManagerTransport.mu.Lock()
	if sharedAgentManagerTransport.conn != nil && sharedAgentManagerTransport.target == target && sharedAgentManagerTransport.token == token {
		client := sharedAgentManagerTransport.client
		sharedAgentManagerTransport.mu.Unlock()
		return client, nil
	}
	sharedAgentManagerTransport.mu.Unlock()

	network, address, err := parseAgentManagerTarget(target)
	if err != nil {
		return nil, err
	}
	opts := agentManagerDialOptions(token)
	var conn *grpc.ClientConn
	switch network {
	case "unix":
		conn, err = grpc.DialContext(ctx, "passthrough:///localhost",
			append([]grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", address)
				}),
				grpc.WithAuthority("localhost"),
				grpc.WithBlock(),
			}, opts...)...,
		)
	case "tcp":
		conn, err = grpc.DialContext(ctx, address,
			append([]grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithBlock(),
			}, opts...)...,
		)
	case "tls":
		host, _, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			return nil, fmt.Errorf("agent manager: parse tls target %q: %w", address, splitErr)
		}
		conn, err = grpc.DialContext(ctx, address,
			append([]grpc.DialOption{
				grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
					MinVersion: tls.VersionTLS12,
					ServerName: host,
					NextProtos: []string{"h2"},
				})),
				grpc.WithBlock(),
			}, opts...)...,
		)
	default:
		return nil, fmt.Errorf("agent manager: unsupported transport network %q", network)
	}
	if err != nil {
		return nil, fmt.Errorf("agent manager: connect to host: %w", err)
	}

	client := proto.NewAgentManagerHostClient(conn)

	sharedAgentManagerTransport.mu.Lock()
	defer sharedAgentManagerTransport.mu.Unlock()

	if sharedAgentManagerTransport.conn != nil && sharedAgentManagerTransport.target == target && sharedAgentManagerTransport.token == token {
		_ = conn.Close()
		return sharedAgentManagerTransport.client, nil
	}
	if sharedAgentManagerTransport.conn != nil {
		_ = sharedAgentManagerTransport.conn.Close()
	}

	sharedAgentManagerTransport.target = target
	sharedAgentManagerTransport.token = token
	sharedAgentManagerTransport.conn = conn
	sharedAgentManagerTransport.client = client
	return client, nil
}

func agentManagerDialOptions(token string) []grpc.DialOption {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return []grpc.DialOption{grpc.WithPerRPCCredentials(agentManagerRelayPerRPCCredentials{token: token})}
}

type agentManagerRelayPerRPCCredentials struct {
	token string
}

func (c agentManagerRelayPerRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{
		"x-gestalt-host-service-relay-token": c.token,
	}, nil
}

func (agentManagerRelayPerRPCCredentials) RequireTransportSecurity() bool { return false }

func parseAgentManagerTarget(raw string) (network string, address string, err error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("agent manager: transport target is required")
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("agent manager: tcp target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "tls://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
		if address == "" {
			return "", "", fmt.Errorf("agent manager: tls target %q is missing host:port", raw)
		}
		return "tls", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("agent manager: unix target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		parsed, parseErr := url.Parse(target)
		if parseErr != nil {
			return "", "", fmt.Errorf("agent manager: parse target %q: %w", raw, parseErr)
		}
		return "", "", fmt.Errorf("agent manager: unsupported target scheme %q", parsed.Scheme)
	default:
		return "unix", filepath.Clean(target), nil
	}
}

func AgentManagerFromContext(ctx context.Context) (*AgentManagerClient, error) {
	return AgentManager(InvocationTokenFromContext(ctx))
}

func (c *AgentManagerClient) Close() error {
	return nil
}

func (c *AgentManagerClient) Run(ctx context.Context, req *proto.AgentManagerRunRequest) (*proto.ManagedAgentRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerRunRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerRunRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.Run(ctx, value)
}

func (c *AgentManagerClient) GetRun(ctx context.Context, req *proto.AgentManagerGetRunRequest) (*proto.ManagedAgentRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerGetRunRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerGetRunRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.GetRun(ctx, value)
}

func (c *AgentManagerClient) ListRuns(ctx context.Context, req *proto.AgentManagerListRunsRequest) (*proto.AgentManagerListRunsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerListRunsRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerListRunsRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ListRuns(ctx, value)
}

func (c *AgentManagerClient) CancelRun(ctx context.Context, req *proto.AgentManagerCancelRunRequest) (*proto.ManagedAgentRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerCancelRunRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerCancelRunRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.CancelRun(ctx, value)
}
