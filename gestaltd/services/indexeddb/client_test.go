package indexeddb

import (
	"context"
	"testing"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	rpcidb "github.com/valon-technologies/gestalt/server/rpc/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type deadlineIndexedDBClient struct {
	proto.IndexedDBClient
	createObjectStore func(context.Context, *proto.CreateObjectStoreRequest, ...grpc.CallOption) (*emptypb.Empty, error)
	deleteObjectStore func(context.Context, *proto.DeleteObjectStoreRequest, ...grpc.CallOption) (*emptypb.Empty, error)
}

func (c *deadlineIndexedDBClient) CreateObjectStore(ctx context.Context, req *proto.CreateObjectStoreRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	return c.createObjectStore(ctx, req, opts...)
}

func (c *deadlineIndexedDBClient) DeleteObjectStore(ctx context.Context, req *proto.DeleteObjectStoreRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	return c.deleteObjectStore(ctx, req, opts...)
}

func TestRemoteIndexedDBSchemaChangesUseProviderRPCTimeout(t *testing.T) {
	t.Parallel()

	assertDeadline := func(t *testing.T, ctx context.Context) {
		t.Helper()
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("schema change context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= runtimehost.ProviderRPCTimeout-2*time.Second || remaining > runtimehost.ProviderRPCTimeout {
			t.Fatalf("schema change deadline remaining = %s, want within 2s of %s", remaining, runtimehost.ProviderRPCTimeout)
		}
	}

	client := &deadlineIndexedDBClient{
		createObjectStore: func(ctx context.Context, _ *proto.CreateObjectStoreRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			assertDeadline(t, ctx)
			return &emptypb.Empty{}, nil
		},
		deleteObjectStore: func(ctx context.Context, _ *proto.DeleteObjectStoreRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			assertDeadline(t, ctx)
			return &emptypb.Empty{}, nil
		},
	}
	db := &remoteIndexedDB{Database: rpcidb.NewClient(client, rpcidb.Options{UnaryTimeout: runtimehost.ProviderRPCTimeout})}

	if _, err := db.CreateObjectStore(context.Background(), "api_tokens", idb.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	if err := db.DeleteObjectStore(context.Background(), "api_tokens"); err != nil {
		t.Fatalf("DeleteObjectStore: %v", err)
	}
}
