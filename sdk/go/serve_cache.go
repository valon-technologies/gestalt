package gestalt

import (
	"context"
	"time"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ServeCacheProvider starts a gRPC server for a [CacheProvider].
func ServeCacheProvider(ctx context.Context, cache CacheProvider) error {
	return serveProvider(withProviderCloser(ctx, cache), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindCache, cache))
		proto.RegisterCacheServer(srv, client.NewCacheProviderServer(cacheHandler{provider: cache}))
	})
}

// cacheHandler bridges the ergonomic [CacheProvider] facade onto the
// generated transport handler; wire conversion lives in the generated
// adapter.
type cacheHandler struct {
	client.UnimplementedCacheProvider
	provider CacheProvider
}

func (s cacheHandler) Get(ctx context.Context, request *client.CacheGetRequest) (*client.CacheGetResponse, error) {
	value, found, err := s.provider.Get(ctx, request.GetKey())
	if err != nil {
		return nil, providerRPCError("cache get", err)
	}
	return &client.CacheGetResponse{Found: found, Value: append([]byte(nil), value...)}, nil
}

func (s cacheHandler) GetMany(ctx context.Context, request *client.CacheGetManyRequest) (*client.CacheGetManyResponse, error) {
	keys := request.GetKeys()
	values, err := s.provider.GetMany(ctx, append([]string(nil), keys...))
	if err != nil {
		return nil, providerRPCError("cache get many", err)
	}
	entries := make([]*client.CacheResult, 0, len(keys))
	for _, key := range keys {
		entry := &client.CacheResult{Key: key}
		if value, ok := values[key]; ok {
			entry.Found = true
			entry.Value = append([]byte(nil), value...)
		}
		entries = append(entries, entry)
	}
	return &client.CacheGetManyResponse{Entries: entries}, nil
}

func (s cacheHandler) Set(ctx context.Context, request *client.CacheSetRequest) error {
	ttl, err := cacheTTL(request.GetTTL())
	if err != nil {
		return err
	}
	if err := s.provider.Set(ctx, request.GetKey(), append([]byte(nil), request.GetValue()...), CacheSetOptions{TTL: ttl}); err != nil {
		return providerRPCError("cache set", err)
	}
	return nil
}

func (s cacheHandler) SetMany(ctx context.Context, request *client.CacheSetManyRequest) error {
	ttl, err := cacheTTL(request.GetTTL())
	if err != nil {
		return err
	}
	entries := make([]CacheEntry, 0, len(request.GetEntries()))
	for _, entry := range request.GetEntries() {
		entries = append(entries, CacheEntry{Key: entry.GetKey(), Value: append([]byte(nil), entry.GetValue()...)})
	}
	if err := s.provider.SetMany(ctx, entries, CacheSetOptions{TTL: ttl}); err != nil {
		return providerRPCError("cache set many", err)
	}
	return nil
}

func (s cacheHandler) Delete(ctx context.Context, request *client.CacheDeleteRequest) (*client.CacheDeleteResponse, error) {
	deleted, err := s.provider.Delete(ctx, request.GetKey())
	if err != nil {
		return nil, providerRPCError("cache delete", err)
	}
	return &client.CacheDeleteResponse{Deleted: deleted}, nil
}

func (s cacheHandler) DeleteMany(ctx context.Context, request *client.CacheDeleteManyRequest) (*client.CacheDeleteManyResponse, error) {
	deleted, err := s.provider.DeleteMany(ctx, append([]string(nil), request.GetKeys()...))
	if err != nil {
		return nil, providerRPCError("cache delete many", err)
	}
	return &client.CacheDeleteManyResponse{Deleted: deleted}, nil
}

func (s cacheHandler) Touch(ctx context.Context, request *client.CacheTouchRequest) (*client.CacheTouchResponse, error) {
	ttl, err := cacheTTL(request.GetTTL())
	if err != nil {
		return nil, err
	}
	touched, err := s.provider.Touch(ctx, request.GetKey(), ttl)
	if err != nil {
		return nil, providerRPCError("cache touch", err)
	}
	return &client.CacheTouchResponse{Touched: touched}, nil
}

func cacheTTL(ttl *time.Duration) (time.Duration, error) {
	if ttl == nil {
		return 0, nil
	}
	if *ttl < 0 {
		return 0, status.Error(codes.InvalidArgument, "cache: ttl must be non-negative")
	}
	return *ttl, nil
}
