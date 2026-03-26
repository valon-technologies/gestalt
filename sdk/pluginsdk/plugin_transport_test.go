package pluginsdk_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type stubRuntimePlugin struct {
	pluginapiv1.UnimplementedRuntimePluginServer
	started int
	stopped int
	lastReq *pluginapiv1.StartRuntimeRequest
}

func (s *stubRuntimePlugin) Start(_ context.Context, req *pluginapiv1.StartRuntimeRequest) (*emptypb.Empty, error) {
	s.started++
	s.lastReq = req
	return &emptypb.Empty{}, nil
}

func (s *stubRuntimePlugin) Stop(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	s.stopped++
	return &emptypb.Empty{}, nil
}

type stubRuntimeHost struct {
	pluginapiv1.UnimplementedRuntimeHostServer
}

func (s *stubRuntimeHost) ListCapabilities(context.Context, *emptypb.Empty) (*pluginapiv1.ListCapabilitiesResponse, error) {
	return &pluginapiv1.ListCapabilitiesResponse{
		Capabilities: []*pluginapiv1.Capability{
			{Provider: "alpha", Operation: "read"},
		},
	}, nil
}

func TestServeProviderRoundTrip(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "plugin.sock")
	t.Setenv(pluginapiv1.EnvPluginSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- pluginsdk.ServeProvider(ctx, &stubProvider{
			name:        "test-provider",
			displayName: "Test Provider",
			connMode:    pluginsdk.ConnectionModeEither,
		})
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
	})

	conn := newUnixConn(t, socket)
	client := pluginapiv1.NewProviderPluginClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), time.Second)
	defer rpcCancel()

	meta, err := client.GetMetadata(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetName() != "test-provider" || meta.GetConnectionMode() != pluginapiv1.ConnectionMode_CONNECTION_MODE_EITHER {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestServeRuntimeRoundTrip(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "runtime.sock")
	t.Setenv(pluginapiv1.EnvPluginSocket, socket)

	server := &stubRuntimePlugin{}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- pluginsdk.ServeRuntime(ctx, server)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
	})

	conn := newUnixConn(t, socket)
	client := pluginapiv1.NewRuntimePluginClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), time.Second)
	defer rpcCancel()

	if _, err := client.Start(rpcCtx, &pluginapiv1.StartRuntimeRequest{Name: "echo"}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := client.Stop(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if server.started != 1 || server.stopped != 1 || server.lastReq.GetName() != "echo" {
		t.Fatalf("unexpected runtime server state: %+v", server)
	}
}

func TestDialRuntimeHostRoundTrip(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "host.sock")
	t.Setenv(pluginapiv1.EnvRuntimeHostSocket, socket)

	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	srv := grpc.NewServer()
	pluginapiv1.RegisterRuntimeHostServer(srv, &stubRuntimeHost{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, client, err := pluginsdk.DialRuntimeHost(context.Background())
	if err != nil {
		t.Fatalf("DialRuntimeHost: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), time.Second)
	defer rpcCancel()

	resp, err := client.ListCapabilities(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("ListCapabilities: %v", err)
	}
	if len(resp.GetCapabilities()) != 1 || resp.GetCapabilities()[0].GetProvider() != "alpha" {
		t.Fatalf("unexpected capabilities: %+v", resp.GetCapabilities())
	}
}
