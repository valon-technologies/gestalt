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

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type hostServiceConnKey struct {
	target string
	token  string
}

type pooledHostServiceConn struct {
	mu     sync.Mutex
	key    hostServiceConnKey
	conn   *grpc.ClientConn
	closed bool
}

var sharedHostServiceConns sync.Map // hostServiceConnKey -> *pooledHostServiceConn

func sharedHostServiceConnFor(ctx context.Context, serviceName, target, token string) (*grpc.ClientConn, error) {
	key := hostServiceConnKey{target: target, token: token}

	for {
		if existing, ok := sharedHostServiceConns.Load(key); ok {
			pooled := existing.(*pooledHostServiceConn)
			pooled.mu.Lock()
			if pooled.conn != nil && !pooled.closed && pooled.key == key {
				conn := pooled.conn
				pooled.mu.Unlock()
				return conn, nil
			}
			pooled.mu.Unlock()
		}

		conn, err := dialHostService(ctx, serviceName, target, token)
		if err != nil {
			return nil, err
		}

		pooled := &pooledHostServiceConn{key: key, conn: conn}
		actual, loaded := sharedHostServiceConns.LoadOrStore(key, pooled)
		if !loaded {
			return conn, nil
		}

		_ = conn.Close()
		holder := actual.(*pooledHostServiceConn)
		holder.mu.Lock()
		if holder.conn != nil && !holder.closed && holder.key == key {
			conn := holder.conn
			holder.mu.Unlock()
			return conn, nil
		}
		holder.mu.Unlock()
	}
}

func cachedHostServiceGRPCClient[C any](
	ctx context.Context,
	serviceName, target, token string,
	cache *sync.Map,
	newClient func(grpc.ClientConnInterface) C,
) (C, error) {
	var zero C
	if cache == nil {
		return zero, fmt.Errorf("%s: client cache is not initialized", serviceName)
	}

	key := hostServiceConnKey{target: target, token: token}
	if existing, ok := cache.Load(key); ok {
		return existing.(C), nil
	}

	conn, err := sharedHostServiceConnFor(ctx, serviceName, target, token)
	if err != nil {
		return zero, err
	}
	client := newClient(conn)

	actual, loaded := cache.LoadOrStore(key, client)
	if loaded {
		return actual.(C), nil
	}
	return client, nil
}

type s3GRPCClients struct {
	client             proto.S3Client
	objectAccessClient proto.S3ObjectAccessClient
}

func cachedS3GRPCClients(ctx context.Context, target, token string, cache *sync.Map) (proto.S3Client, proto.S3ObjectAccessClient, error) {
	if cache == nil {
		return nil, nil, fmt.Errorf("s3: client cache is not initialized")
	}

	key := hostServiceConnKey{target: target, token: token}
	if existing, ok := cache.Load(key); ok {
		clients := existing.(s3GRPCClients)
		return clients.client, clients.objectAccessClient, nil
	}

	conn, err := sharedHostServiceConnFor(ctx, "s3", target, token)
	if err != nil {
		return nil, nil, err
	}
	clients := s3GRPCClients{
		client:             proto.NewS3Client(conn),
		objectAccessClient: proto.NewS3ObjectAccessClient(conn),
	}

	actual, loaded := cache.LoadOrStore(key, clients)
	if loaded {
		clients = actual.(s3GRPCClients)
	}
	return clients.client, clients.objectAccessClient, nil
}

func managerTransportClient[C any](
	ctx context.Context,
	serviceName, target, token string,
	cache *sync.Map,
	newClient func(grpc.ClientConnInterface) C,
) (C, error) {
	return cachedHostServiceGRPCClient(ctx, serviceName, target, token, cache, newClient)
}

func hostServiceCallCtx(ctx context.Context, binding string) context.Context {
	return withHostServiceBinding(ctx, binding)
}

func dialHostService(ctx context.Context, serviceName, target, token string) (*grpc.ClientConn, error) {
	return dialManagerTransport(ctx, serviceName, target, hostServiceDialOptions(token)...)
}

func dialManagerTransport(ctx context.Context, serviceName, target string, extraOpts ...grpc.DialOption) (*grpc.ClientConn, error) {
	network, address, err := parseManagerTransportTarget(serviceName, target)
	if err != nil {
		return nil, err
	}
	switch network {
	case "unix":
		return grpc.DialContext(ctx, "passthrough:///localhost",
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", address)
				}),
				grpc.WithAuthority("localhost"),
				grpc.WithBlock(),
			), extraOpts...)...,
		)
	case "tcp":
		return grpc.DialContext(ctx, address,
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithBlock(),
			), extraOpts...)...,
		)
	case "tls":
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%s: parse tls target %q: %w", serviceName, address, err)
		}
		tlsConfig, err := hostServiceTLSConfig(serviceName, host)
		if err != nil {
			return nil, err
		}
		return grpc.DialContext(ctx, address,
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
				grpc.WithBlock(),
			), extraOpts...)...,
		)
	default:
		return nil, fmt.Errorf("%s: unsupported transport network %q", serviceName, network)
	}
}

func hostServiceTarget(serviceName string) (string, string, error) {
	target := strings.TrimSpace(os.Getenv(EnvHostServiceSocket))
	if target == "" {
		return "", "", fmt.Errorf("%s: %s is not set", serviceName, EnvHostServiceSocket)
	}
	return target, strings.TrimSpace(os.Getenv(EnvHostServiceToken)), nil
}

func hostServiceDialOptions(token string) []grpc.DialOption {
	return []grpc.DialOption{grpc.WithPerRPCCredentials(hostServicePerRPCCredentials{token: strings.TrimSpace(token)})}
}

type hostServicePerRPCCredentials struct {
	token string
}

func (c hostServicePerRPCCredentials) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	md := make(map[string]string, 2)
	if c.token != "" {
		md[hostServiceRelayTokenHeader] = c.token
	}
	if binding := hostServiceBindingFromContext(ctx); binding != "" {
		md[HostServiceBindingMetadata] = binding
	}
	return md, nil
}

func (hostServicePerRPCCredentials) RequireTransportSecurity() bool { return false }

func parseManagerTransportTarget(serviceName, raw string) (network string, address string, err error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("%s: transport target is required", serviceName)
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("%s: tcp target %q is missing host:port", serviceName, raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "tls://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
		if address == "" {
			return "", "", fmt.Errorf("%s: tls target %q is missing host:port", serviceName, raw)
		}
		return "tls", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("%s: unix target %q is missing a socket path", serviceName, raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		parsed, parseErr := url.Parse(target)
		if parseErr != nil {
			return "", "", fmt.Errorf("%s: parse target %q: %w", serviceName, raw, parseErr)
		}
		return "", "", fmt.Errorf("%s: unsupported target scheme %q", serviceName, parsed.Scheme)
	default:
		return "unix", filepath.Clean(target), nil
	}
}
