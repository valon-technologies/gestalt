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
	"google.golang.org/protobuf/types/known/structpb"
)

const EnvPluginInvokerSocket = proto.EnvPluginInvokerSocket
const EnvPluginInvokerSocketToken = EnvPluginInvokerSocket + "_TOKEN"

type InvokeOptions struct {
	Connection string
	Instance   string
}

type InvocationGrant struct {
	Plugin        string
	Operations    []string
	Surfaces      []string
	AllOperations bool
}

type InvokerClient struct {
	client          proto.PluginInvokerClient
	invocationToken string
}

var sharedInvokerTransport struct {
	mu     sync.Mutex
	target string
	token  string
	conn   *grpc.ClientConn
	client proto.PluginInvokerClient
}

func Invoker(invocationToken string) (*InvokerClient, error) {
	if strings.TrimSpace(invocationToken) == "" {
		return nil, fmt.Errorf("plugin invoker: invocation token is not available")
	}
	target := os.Getenv(EnvPluginInvokerSocket)
	if target == "" {
		return nil, fmt.Errorf("plugin invoker: %s is not set", EnvPluginInvokerSocket)
	}
	token := os.Getenv(EnvPluginInvokerSocketToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := sharedPluginInvokerClient(ctx, target, token)
	if err != nil {
		return nil, err
	}

	return &InvokerClient{
		client:          client,
		invocationToken: strings.TrimSpace(invocationToken),
	}, nil
}

func sharedPluginInvokerClient(ctx context.Context, target, token string) (proto.PluginInvokerClient, error) {
	sharedInvokerTransport.mu.Lock()
	if sharedInvokerTransport.conn != nil && sharedInvokerTransport.target == target && sharedInvokerTransport.token == token {
		client := sharedInvokerTransport.client
		sharedInvokerTransport.mu.Unlock()
		return client, nil
	}
	sharedInvokerTransport.mu.Unlock()

	network, address, err := parsePluginInvokerTarget(target)
	if err != nil {
		return nil, err
	}
	opts := pluginInvokerDialOptions(token)
	var conn *grpc.ClientConn
	switch network {
	case "unix":
		conn, err = grpc.DialContext(ctx, "passthrough:///localhost",
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", address)
				}),
				grpc.WithAuthority("localhost"),
				grpc.WithBlock(),
			), opts...)...,
		)
	case "tcp":
		conn, err = grpc.DialContext(ctx, address,
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithBlock(),
			), opts...)...,
		)
	case "tls":
		host, _, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			return nil, fmt.Errorf("plugin invoker: parse tls target %q: %w", address, splitErr)
		}
		conn, err = grpc.DialContext(ctx, address,
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
					MinVersion: tls.VersionTLS12,
					ServerName: host,
					NextProtos: []string{"h2"},
				})),
				grpc.WithBlock(),
			), opts...)...,
		)
	default:
		return nil, fmt.Errorf("plugin invoker: unsupported transport network %q", network)
	}
	if err != nil {
		return nil, fmt.Errorf("plugin invoker: connect to host: %w", err)
	}

	client := proto.NewPluginInvokerClient(conn)

	sharedInvokerTransport.mu.Lock()
	defer sharedInvokerTransport.mu.Unlock()

	if sharedInvokerTransport.conn != nil && sharedInvokerTransport.target == target && sharedInvokerTransport.token == token {
		_ = conn.Close()
		return sharedInvokerTransport.client, nil
	}
	if sharedInvokerTransport.conn != nil {
		_ = sharedInvokerTransport.conn.Close()
	}

	sharedInvokerTransport.target = target
	sharedInvokerTransport.token = token
	sharedInvokerTransport.conn = conn
	sharedInvokerTransport.client = client
	return client, nil
}

func pluginInvokerDialOptions(token string) []grpc.DialOption {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return []grpc.DialOption{grpc.WithPerRPCCredentials(pluginInvokerRelayPerRPCCredentials{token: token})}
}

type pluginInvokerRelayPerRPCCredentials struct {
	token string
}

func (c pluginInvokerRelayPerRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{
		"x-gestalt-host-service-relay-token": c.token,
	}, nil
}

func (pluginInvokerRelayPerRPCCredentials) RequireTransportSecurity() bool { return false }

func parsePluginInvokerTarget(raw string) (network string, address string, err error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("plugin invoker: transport target is required")
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("plugin invoker: tcp target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "tls://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
		if address == "" {
			return "", "", fmt.Errorf("plugin invoker: tls target %q is missing host:port", raw)
		}
		return "tls", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("plugin invoker: unix target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		parsed, parseErr := url.Parse(target)
		if parseErr != nil {
			return "", "", fmt.Errorf("plugin invoker: parse target %q: %w", raw, parseErr)
		}
		return "", "", fmt.Errorf("plugin invoker: unsupported target scheme %q", parsed.Scheme)
	default:
		return "unix", filepath.Clean(target), nil
	}
}

func InvokerFromContext(ctx context.Context) (*InvokerClient, error) {
	return Invoker(InvocationTokenFromContext(ctx))
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
		InvocationToken: c.invocationToken,
		Plugin:          plugin,
		Operation:       operation,
		Params:          msg,
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

func (c *InvokerClient) InvokeGraphQL(ctx context.Context, plugin, document string, variables map[string]any, opts *InvokeOptions) (*OperationResult, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("plugin invoker: client is not initialized")
	}
	document = strings.TrimSpace(document)
	if document == "" {
		return nil, fmt.Errorf("plugin invoker: graphql document is required")
	}

	var msg *structpb.Struct
	var err error
	if len(variables) > 0 {
		msg, err = structpb.NewStruct(variables)
		if err != nil {
			return nil, fmt.Errorf("plugin invoker: encode variables: %w", err)
		}
	}

	req := &proto.PluginInvokeGraphQLRequest{
		InvocationToken: c.invocationToken,
		Plugin:          plugin,
		Document:        document,
		Variables:       msg,
	}
	if opts != nil {
		req.Connection = opts.Connection
		req.Instance = opts.Instance
	}

	resp, err := c.client.InvokeGraphQL(ctx, req)
	if err != nil {
		return nil, err
	}
	return &OperationResult{
		Status: int(resp.GetStatus()),
		Body:   resp.GetBody(),
	}, nil
}

func (c *InvokerClient) ExchangeInvocationToken(ctx context.Context, grants []InvocationGrant, ttl time.Duration) (string, error) {
	if c == nil || c.client == nil {
		return "", fmt.Errorf("plugin invoker: client is not initialized")
	}

	req := &proto.ExchangeInvocationTokenRequest{
		ParentInvocationToken: c.invocationToken,
		Grants:                encodeInvocationGrants(grants),
	}
	if ttl > 0 {
		req.TtlSeconds = int64(ttl / time.Second)
		if req.TtlSeconds == 0 {
			req.TtlSeconds = 1
		}
	}

	resp, err := c.client.ExchangeInvocationToken(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.GetInvocationToken(), nil
}

func encodeInvocationGrants(grants []InvocationGrant) []*proto.PluginInvocationGrant {
	if len(grants) == 0 {
		return nil
	}
	out := make([]*proto.PluginInvocationGrant, 0, len(grants))
	for _, grant := range grants {
		plugin := strings.TrimSpace(grant.Plugin)
		if plugin == "" {
			continue
		}
		ops := make([]string, 0, len(grant.Operations))
		for _, operation := range grant.Operations {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			ops = append(ops, operation)
		}
		out = append(out, &proto.PluginInvocationGrant{
			Plugin:        plugin,
			Operations:    ops,
			Surfaces:      grant.Surfaces,
			AllOperations: grant.AllOperations,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
