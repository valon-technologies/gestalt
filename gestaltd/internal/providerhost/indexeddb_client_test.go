package providerhost

import (
	"context"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
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

func TestRemoteIndexedDBSchemaChangesUseMigrationTimeout(t *testing.T) {
	t.Parallel()

	assertMigrationDeadline := func(t *testing.T, ctx context.Context) {
		t.Helper()
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("schema change context has no deadline")
		}
		if remaining := time.Until(deadline); remaining <= providerRPCTimeout {
			t.Fatalf("schema change deadline remaining = %s, want greater than provider RPC timeout %s", remaining, providerRPCTimeout)
		}
	}

	client := &deadlineIndexedDBClient{
		createObjectStore: func(ctx context.Context, _ *proto.CreateObjectStoreRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			assertMigrationDeadline(t, ctx)
			return &emptypb.Empty{}, nil
		},
		deleteObjectStore: func(ctx context.Context, _ *proto.DeleteObjectStoreRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			assertMigrationDeadline(t, ctx)
			return &emptypb.Empty{}, nil
		},
	}
	db := &remoteIndexedDB{client: client}

	if err := db.CreateObjectStore(context.Background(), "api_tokens", indexeddb.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	if err := db.DeleteObjectStore(context.Background(), "api_tokens"); err != nil {
		t.Fatalf("DeleteObjectStore: %v", err)
	}
}
