package host

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// SharedTransport caches one client over one connection for a fixed dial
// identity, redialing only when the identity changes.
type SharedTransport[C any] struct {
	mu      sync.Mutex
	target  string
	token   string
	binding string
	conn    *grpc.ClientConn
	client  C
}

// ManagerClient returns the shared client for an unbound (manager-level)
// host service, dialing it on first use.
func ManagerClient[C any](ctx context.Context, serviceName, target, token string, transport *SharedTransport[C], newClient func(grpc.ClientConnInterface) C) (C, error) {
	return ServiceClient(ctx, serviceName, target, token, "", transport, newClient)
}

// ServiceClient returns the shared client for a host service binding,
// dialing it on first use and redialing when the dial identity changes.
func ServiceClient[C any](ctx context.Context, serviceName, target, token, binding string, transport *SharedTransport[C], newClient func(grpc.ClientConnInterface) C) (C, error) {
	var zero C
	if transport == nil {
		return zero, fmt.Errorf("%s: shared transport is not initialized", serviceName)
	}

	transport.mu.Lock()
	if transport.conn != nil && transport.target == target && transport.token == token && transport.binding == binding {
		client := transport.client
		transport.mu.Unlock()
		return client, nil
	}
	transport.mu.Unlock()

	conn, err := DialService(ctx, serviceName, target, token, binding)
	if err != nil {
		return zero, err
	}
	client := newClient(conn)

	transport.mu.Lock()
	defer transport.mu.Unlock()

	if transport.conn != nil && transport.target == target && transport.token == token && transport.binding == binding {
		_ = conn.Close()
		return transport.client, nil
	}
	if transport.conn != nil {
		_ = transport.conn.Close()
	}

	transport.target = target
	transport.token = token
	transport.binding = binding
	transport.conn = conn
	transport.client = client
	return client, nil
}

// ConnPool caches one client connection per dial identity (target, token,
// binding). Connections are shared across clients and live for the life of
// the process, mirroring the shared transports of the handwritten clients
// the generated constructors replaced; the pool stays bounded by the set of
// distinct bindings a process uses.
type ConnPool struct {
	mu    sync.Mutex
	conns map[connKey]*grpc.ClientConn
}

type connKey struct {
	target  string
	token   string
	binding string
}

// Conn returns the pooled connection for the dial identity, dialing on first
// use. Concurrent first calls may dial in parallel; losers close their extra
// connection and share the winner's.
func (p *ConnPool) Conn(ctx context.Context, serviceName, target, token, binding string) (*grpc.ClientConn, error) {
	key := connKey{target: target, token: token, binding: binding}
	p.mu.Lock()
	if conn, ok := p.conns[key]; ok {
		p.mu.Unlock()
		return conn, nil
	}
	p.mu.Unlock()

	conn, err := DialService(ctx, serviceName, target, token, binding)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.conns[key]; ok {
		_ = conn.Close()
		return existing, nil
	}
	if p.conns == nil {
		p.conns = map[connKey]*grpc.ClientConn{}
	}
	p.conns[key] = conn
	return conn, nil
}

// DialService opens a new client connection to a host service binding;
// callers own the connection's lifetime.
func DialService(ctx context.Context, serviceName, target, token, binding string) (*grpc.ClientConn, error) {
	return dialTarget(ctx, serviceName, target, dialOptions(token, binding)...)
}

func dialTarget(ctx context.Context, serviceName, target string, extraOpts ...grpc.DialOption) (*grpc.ClientConn, error) {
	network, address, err := parseTarget(serviceName, target)
	if err != nil {
		return nil, err
	}
	switch network {
	case "unix":
		return grpc.DialContext(ctx, "passthrough:///localhost",
			append(baseDialOptions(
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
			append(baseDialOptions(
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithBlock(),
			), extraOpts...)...,
		)
	case "tls":
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%s: parse tls target %q: %w", serviceName, address, err)
		}
		tlsConfig, err := TLSConfig(serviceName, host)
		if err != nil {
			return nil, err
		}
		return grpc.DialContext(ctx, address,
			append(baseDialOptions(
				grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
				grpc.WithBlock(),
			), extraOpts...)...,
		)
	default:
		return nil, fmt.Errorf("%s: unsupported transport network %q", serviceName, network)
	}
}

// Target reads the host service endpoint and relay token the daemon
// advertised through the environment.
func Target(serviceName string) (string, string, error) {
	if raw, ok := os.LookupEnv(EnvHostServices); ok && raw != "" {
		found := false
		for _, name := range strings.Split(raw, ",") {
			if strings.TrimSpace(name) == serviceName {
				found = true
				break
			}
		}
		if !found {
			return "", "", fmt.Errorf("%s: host service is not configured (%s=%q)", serviceName, EnvHostServices, raw)
		}
	}
	target := strings.TrimSpace(os.Getenv(EnvHostServiceSocket))
	if target == "" {
		return "", "", fmt.Errorf("%s: %s is not set", serviceName, EnvHostServiceSocket)
	}
	return target, strings.TrimSpace(os.Getenv(EnvHostServiceToken)), nil
}

// DialOptions returns per-RPC credentials for host-service binding and relay token.
func DialOptions(token string, binding string) []grpc.DialOption {
	return dialOptions(token, binding)
}

func dialOptions(token string, binding string) []grpc.DialOption {
	creds := perRPCCredentials{
		token:   strings.TrimSpace(token),
		binding: strings.TrimSpace(binding),
	}
	if creds.token == "" && creds.binding == "" {
		return nil
	}
	return []grpc.DialOption{grpc.WithPerRPCCredentials(creds)}
}

type perRPCCredentials struct {
	token   string
	binding string
}

func (c perRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	md := make(map[string]string, 2)
	if c.token != "" {
		md[relayTokenHeader] = c.token
	}
	if c.binding != "" {
		md[BindingMetadata] = c.binding
	}
	return md, nil
}

func (perRPCCredentials) RequireTransportSecurity() bool { return false }

func baseDialOptions(base ...grpc.DialOption) []grpc.DialOption {
	opts := make([]grpc.DialOption, 0, len(base)+1)
	opts = append(opts, grpc.WithNoProxy())
	opts = append(opts, base...)
	return opts
}

func parseTarget(serviceName, raw string) (network string, address string, err error) {
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
