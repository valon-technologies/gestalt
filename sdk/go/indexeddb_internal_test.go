package gestalt

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestIndexedDBClientLifecycleNilStatusErrorFramesReturnError(t *testing.T) {
	t.Parallel()

	conn := newIndexedDBTestConn(t, func(s *grpc.Server) {
		proto.RegisterIndexedDBServer(s, malformedLifecycleErrorIndexedDBServer{})
	})
	db := &IndexedDBClient{client: proto.NewIndexedDBClient(conn), conn: conn}

	opened, err := db.Open(context.Background(), "app", OpenOptions{})
	if err == nil {
		if opened != nil {
			_ = opened.Close()
		}
		t.Fatal("Open nil status error frame returned nil error")
	}
	if !strings.Contains(err.Error(), "missing non-OK status") {
		t.Fatalf("Open nil status error = %v, want missing non-OK status", err)
	}

	result, err := db.DeleteDatabase(context.Background(), "app", DeleteOptions{})
	if err == nil {
		t.Fatalf("DeleteDatabase nil status error frame returned nil error and result %#v", result)
	}
	if !strings.Contains(err.Error(), "missing non-OK status") {
		t.Fatalf("DeleteDatabase nil status error = %v, want missing non-OK status", err)
	}
}

func TestIndexedDBClientCanceledStatusMapping(t *testing.T) {
	t.Parallel()

	err := grpcErr(status.Error(codes.Canceled, context.Canceled.Error()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("transport canceled error = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrAbort) {
		t.Fatalf("transport canceled error = %v, should not match ErrAbort", err)
	}

	err = rpcStatusErr(&rpcstatus.Status{Code: int32(codes.Canceled), Message: ErrAbort.Error() + ": user callback"})
	if !errors.Is(err, ErrAbort) {
		t.Fatalf("abort application status error = %v, want ErrAbort", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("abort application status error = %v, should not match context.Canceled", err)
	}

	err = rpcStatusErr(&rpcstatus.Status{Code: int32(codes.Canceled), Message: context.Canceled.Error()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled application status error = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrAbort) {
		t.Fatalf("canceled application status error = %v, should not match ErrAbort", err)
	}
}

func newIndexedDBTestConn(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	register(srv)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
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
