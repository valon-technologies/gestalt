package pluginhost

import (
	"context"
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

// NewResourceProxy creates a [ResourceProxy] that forwards requests from a
// plugin process to the actual resource provider for a single capability, with
// the namespace hardcoded. Only the requested capability is exposed.
func NewResourceProxy(provider *RemoteResourceProvider, namespace string, capability string) (*ResourceProxy, error) {
	rp := &ResourceProxy{}
	switch capability {
	case "key_value":
		kv := provider.KVClient()
		if kv == nil {
			return nil, fmt.Errorf("resource proxy: provider does not support key_value capability")
		}
		rp.KV = &kvProxyServer{client: kv, namespace: namespace}
	case "sql":
		sql := provider.SQLClient()
		if sql == nil {
			return nil, fmt.Errorf("resource proxy: provider does not support sql capability")
		}
		rp.SQL = &sqlProxyServer{client: sql, namespace: namespace}
	case "blob_store":
		blob := provider.BlobClient()
		if blob == nil {
			return nil, fmt.Errorf("resource proxy: provider does not support blob_store capability")
		}
		rp.Blob = &blobProxyServer{client: blob, namespace: namespace}
	default:
		return nil, fmt.Errorf("resource proxy: unknown capability %q", capability)
	}
	return rp, nil
}

// --- KV proxy ---

type kvProxyServer struct {
	proto.UnimplementedKeyValueResourceServer
	client    proto.KeyValueResourceClient
	namespace string
}

func (s *kvProxyServer) Get(ctx context.Context, req *proto.KVGetRequest) (*proto.KVGetResponse, error) {
	req.Namespace = s.namespace
	return s.client.Get(ctx, req)
}

func (s *kvProxyServer) Put(ctx context.Context, req *proto.KVPutRequest) (*emptypb.Empty, error) {
	req.Namespace = s.namespace
	return s.client.Put(ctx, req)
}

func (s *kvProxyServer) Delete(ctx context.Context, req *proto.KVDeleteRequest) (*proto.KVDeleteResponse, error) {
	req.Namespace = s.namespace
	return s.client.Delete(ctx, req)
}

func (s *kvProxyServer) List(ctx context.Context, req *proto.KVListRequest) (*proto.KVListResponse, error) {
	req.Namespace = s.namespace
	return s.client.List(ctx, req)
}

func (s *kvProxyServer) Migrate(ctx context.Context, req *proto.KVMigrateRequest) (*emptypb.Empty, error) {
	req.Namespace = s.namespace
	return s.client.Migrate(ctx, req)
}

// --- SQL proxy ---

type sqlProxyServer struct {
	proto.UnimplementedSQLResourceServer
	client    proto.SQLResourceClient
	namespace string
}

func (s *sqlProxyServer) Query(ctx context.Context, req *proto.SQLQueryRequest) (*proto.SQLQueryResponse, error) {
	req.Namespace = s.namespace
	return s.client.Query(ctx, req)
}

func (s *sqlProxyServer) Exec(ctx context.Context, req *proto.SQLExecRequest) (*proto.SQLExecResponse, error) {
	req.Namespace = s.namespace
	return s.client.Exec(ctx, req)
}

func (s *sqlProxyServer) Migrate(ctx context.Context, req *proto.SQLMigrateRequest) (*emptypb.Empty, error) {
	req.Namespace = s.namespace
	return s.client.Migrate(ctx, req)
}

// --- Blob proxy ---

type blobProxyServer struct {
	proto.UnimplementedBlobStoreResourceServer
	client    proto.BlobStoreResourceClient
	namespace string
}

func (s *blobProxyServer) Get(ctx context.Context, req *proto.BlobGetRequest) (*proto.BlobGetResponse, error) {
	req.Namespace = s.namespace
	return s.client.Get(ctx, req)
}

func (s *blobProxyServer) Put(ctx context.Context, req *proto.BlobPutRequest) (*emptypb.Empty, error) {
	req.Namespace = s.namespace
	return s.client.Put(ctx, req)
}

func (s *blobProxyServer) Delete(ctx context.Context, req *proto.BlobDeleteRequest) (*emptypb.Empty, error) {
	req.Namespace = s.namespace
	return s.client.Delete(ctx, req)
}

func (s *blobProxyServer) List(ctx context.Context, req *proto.BlobListRequest) (*proto.BlobListResponse, error) {
	req.Namespace = s.namespace
	return s.client.List(ctx, req)
}
