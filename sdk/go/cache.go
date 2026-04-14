package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"
)

const EnvCacheSocket = "GESTALT_CACHE_SOCKET"

type CacheEntry struct {
	Key   string
	Value []byte
}

type CacheSetOptions struct {
	TTL time.Duration
}

type CacheClient struct {
	client proto.CacheClient
	conn   *grpc.ClientConn
}

func CacheSocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return EnvCacheSocket
	}
	var b strings.Builder
	b.WriteString(EnvCacheSocket)
	b.WriteByte('_')
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func Cache(name ...string) (*CacheClient, error) {
	envName := EnvCacheSocket
	if len(name) > 0 {
		envName = CacheSocketEnv(name[0])
	}
	socketPath := os.Getenv(envName)
	if socketPath == "" {
		return nil, fmt.Errorf("cache: %s is not set", envName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("cache: connect to host: %w", err)
	}
	return &CacheClient{
		client: proto.NewCacheClient(conn),
		conn:   conn,
	}, nil
}

func (c *CacheClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

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

func (c *CacheClient) Set(ctx context.Context, key string, value []byte, opts CacheSetOptions) error {
	_, err := c.client.Set(ctx, &proto.CacheSetRequest{
		Key:   key,
		Value: append([]byte(nil), value...),
		Ttl:   cacheTTLToProto(opts.TTL),
	})
	return err
}

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

func (c *CacheClient) Delete(ctx context.Context, key string) (bool, error) {
	resp, err := c.client.Delete(ctx, &proto.CacheDeleteRequest{Key: key})
	if err != nil {
		return false, err
	}
	return resp.GetDeleted(), nil
}

func (c *CacheClient) DeleteMany(ctx context.Context, keys []string) (int64, error) {
	resp, err := c.client.DeleteMany(ctx, &proto.CacheDeleteManyRequest{Keys: keys})
	if err != nil {
		return 0, err
	}
	return resp.GetDeleted(), nil
}

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
