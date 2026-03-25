package pluginapi

import (
	"context"
	"net"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/testutil"
	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

type stubRuntimePluginServer struct {
	pluginapiv1.UnimplementedRuntimePluginServer
	startReq *pluginapiv1.StartRuntimeRequest
	started  int
	stopped  int
}

func (s *stubRuntimePluginServer) Start(_ context.Context, req *pluginapiv1.StartRuntimeRequest) (*emptypb.Empty, error) {
	s.startReq = req
	s.started++
	return &emptypb.Empty{}, nil
}

func (s *stubRuntimePluginServer) Stop(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	s.stopped++
	return &emptypb.Empty{}, nil
}

func TestRemoteRuntimeRoundTrip(t *testing.T) {
	t.Parallel()

	stub := &stubRuntimePluginServer{}
	client := newRuntimeTestClient(t, stub)

	rt, err := NewRemoteRuntime("echo", client, map[string]any{"enabled": true}, []core.Capability{
		{Provider: "alpha", Operation: "read"},
	})
	if err != nil {
		t.Fatalf("NewRemoteRuntime: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stub.started != 1 || stub.stopped != 1 {
		t.Fatalf("unexpected runtime call counts start=%d stop=%d", stub.started, stub.stopped)
	}

	if stub.startReq == nil {
		t.Fatal("expected start request to be received")
	}
	if stub.startReq.GetName() != "echo" {
		t.Fatalf("start request name = %q, want echo", stub.startReq.GetName())
	}
	if stub.startReq.GetConfig() == nil || stub.startReq.GetConfig().AsMap()["enabled"] != true {
		t.Fatalf("start request config missing expected 'enabled' key")
	}
	if len(stub.startReq.GetInitialCapabilities()) != 1 || stub.startReq.GetInitialCapabilities()[0].GetProvider() != "alpha" {
		t.Fatalf("start request capabilities = %v, want [{alpha read}]", stub.startReq.GetInitialCapabilities())
	}
}

func TestRuntimeHostServer(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: 202, Body: "ok"},
	}
	lister := &stubCapabilityLister{
		caps: []core.Capability{{Provider: "alpha", Operation: "read"}},
	}

	client := newRuntimeHostClient(t, NewRuntimeHostServer(invoker, lister))

	resp, err := client.ListCapabilities(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("ListCapabilities: %v", err)
	}
	if len(resp.GetCapabilities()) != 1 || resp.GetCapabilities()[0].GetProvider() != "alpha" {
		t.Fatalf("unexpected capabilities: %+v", resp.GetCapabilities())
	}

	result, err := client.Invoke(context.Background(), &pluginapiv1.InvokeRequest{
		Principal: &pluginapiv1.Principal{
			UserId: "user-123",
			Source: pluginapiv1.PrincipalSource_PRINCIPAL_SOURCE_API_TOKEN,
		},
		Provider:  "alpha",
		Operation: "read",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.GetStatus() != 202 || result.GetBody() != "ok" {
		t.Fatalf("unexpected invoke result: %+v", result)
	}
	if !invoker.Invoked || invoker.Provider != "alpha" || invoker.Operation != "read" {
		t.Fatalf("unexpected invoker call: %+v", invoker)
	}
	if invoker.LastP == nil || invoker.LastP.UserID != "user-123" || invoker.LastP.Source != principal.SourceAPIToken {
		t.Fatalf("unexpected principal: %+v", invoker.LastP)
	}
}

type stubCapabilityLister struct {
	caps []core.Capability
}

func (s *stubCapabilityLister) ListCapabilities() []core.Capability { return s.caps }

func newRuntimeTestClient(t *testing.T, server pluginapiv1.RuntimePluginServer) pluginapiv1.RuntimePluginClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pluginapiv1.RegisterRuntimePluginServer(srv, server)
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
	return pluginapiv1.NewRuntimePluginClient(conn)
}

func newRuntimeHostClient(t *testing.T, host pluginapiv1.RuntimeHostServer) pluginapiv1.RuntimeHostClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pluginapiv1.RegisterRuntimeHostServer(srv, host)
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
	return pluginapiv1.NewRuntimeHostClient(conn)
}
