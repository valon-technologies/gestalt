package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ServeCacheProvider starts a gRPC server for a [CacheProvider].
func ServeCacheProvider(ctx context.Context, cache CacheProvider) error {
	return serveProvider(withProviderCloser(ctx, cache), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindCache, cache))
		proto.RegisterCacheServer(srv, cacheProviderServer{provider: cache})
	})
}

type cacheProviderServer struct {
	proto.UnimplementedCacheServer
	provider CacheProvider
}

func (s cacheProviderServer) Get(ctx context.Context, req *proto.CacheGetRequest) (*proto.CacheGetResponse, error) {
	value, found, err := s.provider.Get(ctx, req.GetKey())
	if err != nil {
		return nil, providerRPCError("cache get", err)
	}
	return &proto.CacheGetResponse{Found: found, Value: append([]byte(nil), value...)}, nil
}

func (s cacheProviderServer) GetMany(ctx context.Context, req *proto.CacheGetManyRequest) (*proto.CacheGetManyResponse, error) {
	values, err := s.provider.GetMany(ctx, append([]string(nil), req.GetKeys()...))
	if err != nil {
		return nil, providerRPCError("cache get many", err)
	}
	entries := make([]*proto.CacheResult, 0, len(req.GetKeys()))
	for _, key := range req.GetKeys() {
		entry := &proto.CacheResult{Key: key}
		if value, ok := values[key]; ok {
			entry.Found = true
			entry.Value = append([]byte(nil), value...)
		}
		entries = append(entries, entry)
	}
	return &proto.CacheGetManyResponse{Entries: entries}, nil
}

func (s cacheProviderServer) Set(ctx context.Context, req *proto.CacheSetRequest) (*emptypb.Empty, error) {
	ttl, err := cacheTTLFromProto(req.GetTtl())
	if err != nil {
		return nil, err
	}
	if err := s.provider.Set(ctx, req.GetKey(), append([]byte(nil), req.GetValue()...), CacheSetOptions{TTL: ttl}); err != nil {
		return nil, providerRPCError("cache set", err)
	}
	return &emptypb.Empty{}, nil
}

func (s cacheProviderServer) SetMany(ctx context.Context, req *proto.CacheSetManyRequest) (*emptypb.Empty, error) {
	ttl, err := cacheTTLFromProto(req.GetTtl())
	if err != nil {
		return nil, err
	}
	entries := make([]CacheEntry, 0, len(req.GetEntries()))
	for _, entry := range req.GetEntries() {
		entries = append(entries, CacheEntry{Key: entry.GetKey(), Value: append([]byte(nil), entry.GetValue()...)})
	}
	if err := s.provider.SetMany(ctx, entries, CacheSetOptions{TTL: ttl}); err != nil {
		return nil, providerRPCError("cache set many", err)
	}
	return &emptypb.Empty{}, nil
}

func (s cacheProviderServer) Delete(ctx context.Context, req *proto.CacheDeleteRequest) (*proto.CacheDeleteResponse, error) {
	deleted, err := s.provider.Delete(ctx, req.GetKey())
	if err != nil {
		return nil, providerRPCError("cache delete", err)
	}
	return &proto.CacheDeleteResponse{Deleted: deleted}, nil
}

func (s cacheProviderServer) DeleteMany(ctx context.Context, req *proto.CacheDeleteManyRequest) (*proto.CacheDeleteManyResponse, error) {
	deleted, err := s.provider.DeleteMany(ctx, append([]string(nil), req.GetKeys()...))
	if err != nil {
		return nil, providerRPCError("cache delete many", err)
	}
	return &proto.CacheDeleteManyResponse{Deleted: deleted}, nil
}

func (s cacheProviderServer) Touch(ctx context.Context, req *proto.CacheTouchRequest) (*proto.CacheTouchResponse, error) {
	ttl, err := cacheTTLFromProto(req.GetTtl())
	if err != nil {
		return nil, err
	}
	touched, err := s.provider.Touch(ctx, req.GetKey(), ttl)
	if err != nil {
		return nil, providerRPCError("cache touch", err)
	}
	return &proto.CacheTouchResponse{Touched: touched}, nil
}

func cacheTTLFromProto(ttl *durationpb.Duration) (time.Duration, error) {
	if ttl == nil {
		return 0, nil
	}
	value := ttl.AsDuration()
	if value < 0 {
		return 0, status.Error(codes.InvalidArgument, "cache: ttl must be non-negative")
	}
	return value, nil
}
