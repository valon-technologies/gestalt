package providerhost

import (
	"context"
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type cacheServer struct {
	proto.UnimplementedCacheServer
	cache  corecache.Cache
	plugin string
}

func NewCacheServer(cache corecache.Cache, pluginName string) proto.CacheServer {
	return &cacheServer{cache: cache, plugin: pluginName}
}

func (s *cacheServer) Get(ctx context.Context, req *proto.CacheGetRequest) (*proto.CacheGetResponse, error) {
	value, found, err := s.cache.Get(ctx, s.key(req.GetKey()))
	if err != nil {
		return nil, err
	}
	return &proto.CacheGetResponse{Found: found, Value: value}, nil
}

func (s *cacheServer) GetMany(ctx context.Context, req *proto.CacheGetManyRequest) (*proto.CacheGetManyResponse, error) {
	keys := req.GetKeys()
	namespaced := make([]string, 0, len(keys))
	for _, key := range keys {
		namespaced = append(namespaced, s.key(key))
	}
	values, err := s.cache.GetMany(ctx, namespaced)
	if err != nil {
		return nil, err
	}
	entries := make([]*proto.CacheResult, 0, len(keys))
	for _, key := range keys {
		entry := &proto.CacheResult{Key: key}
		if value, ok := values[s.key(key)]; ok {
			entry.Found = true
			entry.Value = value
		}
		entries = append(entries, entry)
	}
	return &proto.CacheGetManyResponse{Entries: entries}, nil
}

func (s *cacheServer) Set(ctx context.Context, req *proto.CacheSetRequest) (*emptypb.Empty, error) {
	ttl, err := ttlFromProto(req.GetTtl())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.cache.Set(ctx, s.key(req.GetKey()), req.GetValue(), corecache.SetOptions{TTL: ttl}); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *cacheServer) SetMany(ctx context.Context, req *proto.CacheSetManyRequest) (*emptypb.Empty, error) {
	ttl, err := ttlFromProto(req.GetTtl())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	entries := make([]corecache.Entry, 0, len(req.GetEntries()))
	for _, entry := range req.GetEntries() {
		entries = append(entries, corecache.Entry{
			Key:   s.key(entry.GetKey()),
			Value: entry.GetValue(),
		})
	}
	if err := s.cache.SetMany(ctx, entries, corecache.SetOptions{TTL: ttl}); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *cacheServer) Delete(ctx context.Context, req *proto.CacheDeleteRequest) (*proto.CacheDeleteResponse, error) {
	deleted, err := s.cache.Delete(ctx, s.key(req.GetKey()))
	if err != nil {
		return nil, err
	}
	return &proto.CacheDeleteResponse{Deleted: deleted}, nil
}

func (s *cacheServer) DeleteMany(ctx context.Context, req *proto.CacheDeleteManyRequest) (*proto.CacheDeleteManyResponse, error) {
	keys := make([]string, 0, len(req.GetKeys()))
	for _, key := range req.GetKeys() {
		keys = append(keys, s.key(key))
	}
	deleted, err := s.cache.DeleteMany(ctx, keys)
	if err != nil {
		return nil, err
	}
	return &proto.CacheDeleteManyResponse{Deleted: deleted}, nil
}

func (s *cacheServer) Touch(ctx context.Context, req *proto.CacheTouchRequest) (*proto.CacheTouchResponse, error) {
	ttl, err := ttlFromProto(req.GetTtl())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	touched, err := s.cache.Touch(ctx, s.key(req.GetKey()), ttl)
	if err != nil {
		return nil, err
	}
	return &proto.CacheTouchResponse{Touched: touched}, nil
}

func (s *cacheServer) key(key string) string {
	if s.plugin == "" {
		return key
	}
	return fmt.Sprintf("%s:%s", s.plugin, key)
}
