package gestalt

import (
	"context"
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
	"google.golang.org/protobuf/types/known/emptypb"
)

const EnvAuthorizationSocket = "GESTALT_AUTHORIZATION_SOCKET"
const EnvAuthorizationSocketToken = EnvAuthorizationSocket + "_TOKEN"

type AuthorizationClient struct {
	client proto.AuthorizationProviderClient
}

var sharedAuthorizationTransport struct {
	mu     sync.Mutex
	target string
	token  string
	conn   *grpc.ClientConn
	client proto.AuthorizationProviderClient
}

func Authorization() (*AuthorizationClient, error) {
	target := os.Getenv(EnvAuthorizationSocket)
	if target == "" {
		return nil, fmt.Errorf("authorization: %s is not set", EnvAuthorizationSocket)
	}
	token := os.Getenv(EnvAuthorizationSocketToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := sharedAuthorizationClient(ctx, target, token)
	if err != nil {
		return nil, err
	}
	return &AuthorizationClient{client: client}, nil
}

func sharedAuthorizationClient(ctx context.Context, target, token string) (proto.AuthorizationProviderClient, error) {
	sharedAuthorizationTransport.mu.Lock()
	if sharedAuthorizationTransport.conn != nil && sharedAuthorizationTransport.target == target && sharedAuthorizationTransport.token == token {
		client := sharedAuthorizationTransport.client
		sharedAuthorizationTransport.mu.Unlock()
		return client, nil
	}
	sharedAuthorizationTransport.mu.Unlock()

	network, address, err := parseAuthorizationTarget(target)
	if err != nil {
		return nil, err
	}
	opts := authorizationDialOptions(token)
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
			return nil, fmt.Errorf("authorization: parse tls target %q: %w", address, splitErr)
		}
		tlsConfig, tlsErr := hostServiceTLSConfig("authorization", host)
		if tlsErr != nil {
			return nil, tlsErr
		}
		conn, err = grpc.DialContext(ctx, address,
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
				grpc.WithBlock(),
			), opts...)...,
		)
	default:
		return nil, fmt.Errorf("authorization: unsupported transport network %q", network)
	}
	if err != nil {
		return nil, fmt.Errorf("authorization: connect to host: %w", err)
	}

	client := proto.NewAuthorizationProviderClient(conn)

	sharedAuthorizationTransport.mu.Lock()
	defer sharedAuthorizationTransport.mu.Unlock()

	if sharedAuthorizationTransport.conn != nil && sharedAuthorizationTransport.target == target && sharedAuthorizationTransport.token == token {
		_ = conn.Close()
		return sharedAuthorizationTransport.client, nil
	}
	if sharedAuthorizationTransport.conn != nil {
		_ = sharedAuthorizationTransport.conn.Close()
	}

	sharedAuthorizationTransport.target = target
	sharedAuthorizationTransport.token = token
	sharedAuthorizationTransport.conn = conn
	sharedAuthorizationTransport.client = client
	return client, nil
}

func authorizationDialOptions(token string) []grpc.DialOption {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return []grpc.DialOption{grpc.WithPerRPCCredentials(authorizationRelayPerRPCCredentials{token: token})}
}

type authorizationRelayPerRPCCredentials struct {
	token string
}

func (c authorizationRelayPerRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{
		"x-gestalt-host-service-relay-token": c.token,
	}, nil
}

func (authorizationRelayPerRPCCredentials) RequireTransportSecurity() bool { return false }

func parseAuthorizationTarget(raw string) (network string, address string, err error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("authorization: transport target is required")
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("authorization: tcp target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "tls://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
		if address == "" {
			return "", "", fmt.Errorf("authorization: tls target %q is missing host:port", raw)
		}
		return "tls", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("authorization: unix target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		parsed, parseErr := url.Parse(target)
		if parseErr != nil {
			return "", "", fmt.Errorf("authorization: parse target %q: %w", raw, parseErr)
		}
		return "", "", fmt.Errorf("authorization: unsupported target scheme %q", parsed.Scheme)
	default:
		return "unix", filepath.Clean(target), nil
	}
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
