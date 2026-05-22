package gestalt

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"
)

// EnvCacheSocket is deprecated. Host-service clients now read
// [EnvHostServiceSocket].
const EnvCacheSocket = EnvHostServiceSocket
const cacheSocketTokenSuffix = "_TOKEN"

// CacheEntry is one key/value pair written through [CacheClient.SetMany].
type CacheEntry struct {
	Key   string
	Value []byte
}

// CacheSetOptions controls cache writes.
type CacheSetOptions struct {
	TTL time.Duration
}

// CacheClient speaks to a running cache provider over a Unix socket.
type CacheClient struct {
	client proto.CacheClient
	conn   *grpc.ClientConn
}

// CacheSocketEnv returns the environment variable name used for a named cache
// transport socket.
func CacheSocketEnv(name string) string {
	return EnvHostServiceSocket
}

// CacheSocketTokenEnv returns the environment variable name used for a named
// cache relay token.
func CacheSocketTokenEnv(name string) string {
	return EnvHostServiceToken
}

// Cache connects to the cache provider exposed by gestaltd. The target can be
// a plain Unix socket path, a unix:///path URI, or a tcp://host:port or
// tls://host:port URI.
func Cache(name ...string) (*CacheClient, error) {
	target, token, err := hostServiceTarget("cache")
	if err != nil {
		return nil, err
	}
	network, address, err := parseCacheTarget(target)
	if err != nil {
		return nil, err
	}
	binding := firstCacheName(name)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var conn *grpc.ClientConn
	opts := cacheDialOptions(token, binding)
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
			return nil, fmt.Errorf("cache: parse tls target %q: %w", address, splitErr)
		}
		tlsConfig, tlsErr := hostServiceTLSConfig("cache", host)
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
		return nil, fmt.Errorf("cache: unsupported transport network %q", network)
	}
	if err != nil {
		return nil, fmt.Errorf("cache: connect to host: %w", err)
	}
	return &CacheClient{
		client: proto.NewCacheClient(conn),
		conn:   conn,
	}, nil
}

// Close closes the underlying gRPC transport.
func (c *CacheClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Get loads one cached value.
func (c *CacheClient) Get(ctx context.Context, key string) ([]byte, bool, error) {
	resp, err := c.client.Get(ctx, &proto.CacheGetRequest{Key: key})
	if err != nil {
		return nil, false, err
	}
	if !resp.GetFound() {
		return nil, false, nil
	}
	return append([]byte(nil), resp.GetValue()...), true, nil
}

// GetMany loads all present values for keys.
func (c *CacheClient) GetMany(ctx context.Context, keys []string) (map[string][]byte, error) {
	resp, err := c.client.GetMany(ctx, &proto.CacheGetManyRequest{Keys: keys})
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(resp.GetEntries()))
	for _, entry := range resp.GetEntries() {
		if !entry.GetFound() {
			continue
		}
		out[entry.GetKey()] = append([]byte(nil), entry.GetValue()...)
	}
	return out, nil
}

// Set stores one value, replacing any existing entry at key.
func (c *CacheClient) Set(ctx context.Context, key string, value []byte, opts CacheSetOptions) error {
	_, err := c.client.Set(ctx, &proto.CacheSetRequest{
		Key:   key,
		Value: append([]byte(nil), value...),
		Ttl:   cacheTTLToProto(opts.TTL),
	})
	return err
}

// SetMany stores multiple entries in one RPC.
func (c *CacheClient) SetMany(ctx context.Context, entries []CacheEntry, opts CacheSetOptions) error {
	protoEntries := make([]*proto.CacheSetEntry, 0, len(entries))
	for _, entry := range entries {
		protoEntries = append(protoEntries, &proto.CacheSetEntry{
			Key:   entry.Key,
			Value: append([]byte(nil), entry.Value...),
		})
	}
	_, err := c.client.SetMany(ctx, &proto.CacheSetManyRequest{
		Entries: protoEntries,
		Ttl:     cacheTTLToProto(opts.TTL),
	})
	return err
}

// Delete removes one cached value and reports whether it existed.
func (c *CacheClient) Delete(ctx context.Context, key string) (bool, error) {
	resp, err := c.client.Delete(ctx, &proto.CacheDeleteRequest{Key: key})
	if err != nil {
		return false, err
	}
	return resp.GetDeleted(), nil
}

// DeleteMany removes multiple cached values and reports how many were deleted.
func (c *CacheClient) DeleteMany(ctx context.Context, keys []string) (int64, error) {
	resp, err := c.client.DeleteMany(ctx, &proto.CacheDeleteManyRequest{Keys: keys})
	if err != nil {
		return 0, err
	}
	return resp.GetDeleted(), nil
}

// Touch updates the TTL for one cached value.
func (c *CacheClient) Touch(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	resp, err := c.client.Touch(ctx, &proto.CacheTouchRequest{Key: key, Ttl: cacheTTLToProto(ttl)})
	if err != nil {
		return false, err
	}
	return resp.GetTouched(), nil
}

func cacheTTLToProto(ttl time.Duration) *durationpb.Duration {
	if ttl <= 0 {
		return nil
	}
	return durationpb.New(ttl)
}

func cacheDialOptions(token string, binding string) []grpc.DialOption {
	return hostServiceDialOptions(token, binding)
}

func firstCacheName(name []string) string {
	if len(name) == 0 {
		return ""
	}
	return name[0]
}

type cacheRelayPerRPCCredentials struct {
	token string
}

func (c cacheRelayPerRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{
		"x-gestalt-host-service-relay-token": c.token,
	}, nil
}

func (cacheRelayPerRPCCredentials) RequireTransportSecurity() bool { return false }

func parseCacheTarget(raw string) (network string, address string, err error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("cache: transport target is required")
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("cache: tcp target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "tls://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
		if address == "" {
			return "", "", fmt.Errorf("cache: tls target %q is missing host:port", raw)
		}
		return "tls", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("cache: unix target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		parsed, parseErr := url.Parse(target)
		if parseErr != nil {
			return "", "", fmt.Errorf("cache: parse target %q: %w", raw, parseErr)
		}
		return "", "", fmt.Errorf("cache: unsupported target scheme %q", parsed.Scheme)
	default:
		return "unix", filepath.Clean(target), nil
	}
}
