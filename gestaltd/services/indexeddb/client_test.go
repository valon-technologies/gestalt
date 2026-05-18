package indexeddb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	db := &remoteIndexedDB{client: client}

	if err := db.CreateObjectStore(context.Background(), "api_tokens", indexeddb.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	if err := db.DeleteObjectStore(context.Background(), "api_tokens"); err != nil {
		t.Fatalf("DeleteObjectStore: %v", err)
	}
}

func TestRemoteIndexedDBLifecycleNilStatusErrorFramesReturnError(t *testing.T) {
	t.Parallel()

	conn := newBufconnConn(t, func(s *grpc.Server) {
		proto.RegisterIndexedDBServer(s, malformedLifecycleErrorIndexedDBServer{})
	})
	db := &remoteIndexedDB{client: proto.NewIndexedDBClient(conn)}

	opened, err := db.Open(context.Background(), "app", indexeddb.OpenOptions{})
	if err == nil {
		if opened != nil {
			_ = opened.Close()
		}
		t.Fatal("Open nil status error frame returned nil error")
	}
	if !strings.Contains(err.Error(), "missing non-OK status") {
		t.Fatalf("Open nil status error = %v, want missing non-OK status", err)
	}

	result, err := db.DeleteDatabase(context.Background(), "app", indexeddb.DeleteOptions{})
	if err == nil {
		t.Fatalf("DeleteDatabase nil status error frame returned nil error and result %#v", result)
	}
	if !strings.Contains(err.Error(), "missing non-OK status") {
		t.Fatalf("DeleteDatabase nil status error = %v, want missing non-OK status", err)
	}
}

func TestRemoteIndexedDBCanceledStatusMapping(t *testing.T) {
	t.Parallel()

	err := grpcToDatastoreErr(status.Error(codes.Canceled, context.Canceled.Error()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("transport canceled error = %v, want context.Canceled", err)
	}
	if errors.Is(err, indexeddb.ErrAbort) {
		t.Fatalf("transport canceled error = %v, should not match ErrAbort", err)
	}

	err = rpcStatusToDatastoreErr(&rpcstatus.Status{Code: int32(codes.Canceled), Message: indexeddb.ErrAbort.Error() + ": user callback"})
	if !errors.Is(err, indexeddb.ErrAbort) {
		t.Fatalf("abort application status error = %v, want ErrAbort", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("abort application status error = %v, should not match context.Canceled", err)
	}

	err = rpcStatusToDatastoreErr(&rpcstatus.Status{Code: int32(codes.Canceled), Message: context.Canceled.Error()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled application status error = %v, want context.Canceled", err)
	}
	if errors.Is(err, indexeddb.ErrAbort) {
		t.Fatalf("canceled application status error = %v, should not match ErrAbort", err)
	}
}

type malformedLifecycleErrorIndexedDBServer struct {
	proto.UnimplementedIndexedDBServer
}

func (malformedLifecycleErrorIndexedDBServer) OpenDatabase(stream proto.IndexedDB_OpenDatabaseServer) error {
	if _, err := stream.Recv(); err != nil {
		return err
	}
	return stream.Send(&proto.OpenDatabaseServerMessage{
		Msg: &proto.OpenDatabaseServerMessage_Error{},
	})
}

func (malformedLifecycleErrorIndexedDBServer) DeleteDatabase(_ *proto.DeleteDatabaseRequest, stream proto.IndexedDB_DeleteDatabaseServer) error {
	return stream.Send(&proto.DeleteDatabaseServerMessage{
		Msg: &proto.DeleteDatabaseServerMessage_Error{},
	})
}
