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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type sharedManagerTransport[C any] struct {
	mu      sync.Mutex
	target  string
	token   string
	binding string
	conn    *grpc.ClientConn
	client  C
}

func managerTransportClient[C any](ctx context.Context, serviceName, target, token string, transport *sharedManagerTransport[C], newClient func(grpc.ClientConnInterface) C) (C, error) {
	return hostServiceTransportClient(ctx, serviceName, target, token, "", transport, newClient)
}

func hostServiceTransportClient[C any](ctx context.Context, serviceName, target, token, binding string, transport *sharedManagerTransport[C], newClient func(grpc.ClientConnInterface) C) (C, error) {
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

	conn, err := dialHostService(ctx, serviceName, target, token, binding)
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

func dialHostService(ctx context.Context, serviceName, target, token, binding string) (*grpc.ClientConn, error) {
	return dialManagerTransport(ctx, serviceName, target, hostServiceDialOptions(token, binding)...)
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

func hostServiceDialOptions(token string, binding string) []grpc.DialOption {
	creds := hostServicePerRPCCredentials{
		token:   strings.TrimSpace(token),
		binding: strings.TrimSpace(binding),
	}
	if creds.token == "" && creds.binding == "" {
		return nil
	}
	return []grpc.DialOption{grpc.WithPerRPCCredentials(creds)}
}

type hostServicePerRPCCredentials struct {
	token   string
	binding string
}

func (c hostServicePerRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	md := make(map[string]string, 2)
	if c.token != "" {
		md[hostServiceRelayTokenHeader] = c.token
	}
	if c.binding != "" {
		md[HostServiceBindingMetadata] = c.binding
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
