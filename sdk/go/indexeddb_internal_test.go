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

func TestIndexedDBProviderUpgradeOperationErrorsAreRecoverable(t *testing.T) {
	t.Parallel()

	upgrade := &recordingUpgradeContext{stores: map[string]struct{}{"items": {}}}
	resp, terminal, opErr := executeIndexedDBUpgradeOperation(context.Background(), upgrade, &proto.UpgradeOperation{
		RequestId: 1,
		Op: &proto.UpgradeOperation_CreateObjectStore{CreateObjectStore: &proto.UpgradeCreateObjectStoreRequest{
			Name: "items",
		}},
	})
	if opErr != nil {
		t.Fatalf("execute duplicate CreateObjectStore opErr = %v, want nil", opErr)
	}
	if terminal {
		t.Fatal("duplicate CreateObjectStore terminal = true, want false")
	}
	if resp.GetError() == nil {
		t.Fatal("duplicate CreateObjectStore response error is nil")
	}

	resp, terminal, opErr = executeIndexedDBUpgradeOperation(context.Background(), upgrade, &proto.UpgradeOperation{
		RequestId: 2,
		Op:        &proto.UpgradeOperation_FinishUpgrade{FinishUpgrade: &proto.FinishUpgradeRequest{}},
	})
	if opErr != nil {
		t.Fatalf("finish opErr = %v, want nil", opErr)
	}
	if !terminal {
		t.Fatal("finish terminal = false, want true")
	}
	if resp.GetError() != nil {
		t.Fatalf("finish response error = %v, want nil", resp.GetError())
	}
}

type recordingUpgradeContext struct {
	stores map[string]struct{}
}

func (u *recordingUpgradeContext) OldVersion() uint64 { return 1 }

func (u *recordingUpgradeContext) NewVersion() uint64 { return 2 }

func (u *recordingUpgradeContext) Database() UpgradeDatabase { return recordingUpgradeDatabase{u} }

func (u *recordingUpgradeContext) ObjectStoreNames(context.Context) ([]string, error) {
	names := make([]string, 0, len(u.stores))
	for name := range u.stores {
		names = append(names, name)
	}
	return names, nil
}

func (u *recordingUpgradeContext) CreateObjectStore(_ context.Context, name string, _ ObjectStoreSchema) error {
	if _, exists := u.stores[name]; exists {
		return ErrAlreadyExists
	}
	u.stores[name] = struct{}{}
	return nil
}

func (u *recordingUpgradeContext) DeleteObjectStore(_ context.Context, name string) error {
	if _, exists := u.stores[name]; !exists {
		return ErrNotFound
	}
	delete(u.stores, name)
	return nil
}

func (u *recordingUpgradeContext) CreateIndex(context.Context, string, IndexSchema) error {
	return nil
}

func (u *recordingUpgradeContext) DeleteIndex(context.Context, string, string) error {
	return nil
}

type recordingUpgradeDatabase struct {
	*recordingUpgradeContext
}

func (recordingUpgradeDatabase) Name() string { return "app" }

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
