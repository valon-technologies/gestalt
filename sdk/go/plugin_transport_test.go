package gestalt_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type stubRuntime struct {
	started      int
	stopped      int
	lastName     string
	lastInitCaps []gestalt.Capability
}

func (s *stubRuntime) Start(_ context.Context, name string, _ map[string]any, caps []gestalt.Capability, _ gestalt.RuntimeHost) error {
	s.started++
	s.lastName = name
	s.lastInitCaps = caps
	return nil
}

func (s *stubRuntime) Stop(context.Context) error {
	s.stopped++
	return nil
}

type stubRuntimeHost struct {
	proto.UnimplementedRuntimeHostServer
}

func (s *stubRuntimeHost) ListCapabilities(context.Context, *emptypb.Empty) (*proto.ListCapabilitiesResponse, error) {
	return &proto.ListCapabilitiesResponse{
		Capabilities: []*proto.Capability{
			{Provider: "alpha", Operation: "read"},
		},
	}, nil
}

func TestServeProviderRoundTrip(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "plugin.sock")
	t.Setenv(proto.EnvPluginSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeProvider(ctx, &stubProvider{
			name:        "test-provider",
			displayName: "Test Provider",
			connMode:    gestalt.ConnectionModeEither,
		})
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
	})

	conn := newUnixConn(t, socket)
	client := proto.NewProviderPluginClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), time.Second)
	defer rpcCancel()

	meta, err := client.GetMetadata(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetName() != "test-provider" || meta.GetConnectionMode() != proto.ConnectionMode_CONNECTION_MODE_EITHER {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func newRuntimeTestEnv(t *testing.T, rt gestalt.Runtime) proto.RuntimePluginClient {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "sdk-rt-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	pluginSocket := filepath.Join(dir, "rt.sock")
	hostSocket := filepath.Join(dir, "host.sock")
	t.Setenv(proto.EnvPluginSocket, pluginSocket)
	t.Setenv(proto.EnvRuntimeHostSocket, hostSocket)

	hostLis, err := net.Listen("unix", hostSocket)
	if err != nil {
		t.Fatalf("net.Listen host: %v", err)
	}
	t.Cleanup(func() { _ = hostLis.Close() })

	hostSrv := grpc.NewServer()
	proto.RegisterRuntimeHostServer(hostSrv, &stubRuntimeHost{})
	go func() { _ = hostSrv.Serve(hostLis) }()
	t.Cleanup(hostSrv.Stop)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeRuntime(ctx, rt)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
	})

	conn := newUnixConn(t, pluginSocket)
	return proto.NewRuntimePluginClient(conn)
}

func TestServeRuntimeRoundTrip(t *testing.T) {
	rt := &stubRuntime{}
	client := newRuntimeTestEnv(t, rt)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), time.Second)
	defer rpcCancel()

	startReq := &proto.StartRuntimeRequest{
		Name: "echo",
		InitialCapabilities: []*proto.Capability{
			{Provider: "beta", Operation: "write", Description: "test cap"},
		},
	}
	if _, err := client.Start(rpcCtx, startReq, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := client.Stop(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if rt.started != 1 || rt.stopped != 1 || rt.lastName != "echo" {
		t.Fatalf("unexpected runtime state: started=%d stopped=%d lastName=%q", rt.started, rt.stopped, rt.lastName)
	}
	if len(rt.lastInitCaps) != 1 || rt.lastInitCaps[0].Provider != "beta" || rt.lastInitCaps[0].Operation != "write" {
		t.Fatalf("unexpected initial capabilities: %+v", rt.lastInitCaps)
	}
}

func TestServeRuntimeHostIntegration(t *testing.T) {
	var capturedHost gestalt.RuntimeHost
	capturingRT := &capturingRuntime{host: &capturedHost}
	client := newRuntimeTestEnv(t, capturingRT)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rpcCancel()

	if _, err := client.Start(rpcCtx, &proto.StartRuntimeRequest{Name: "test-rt"}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if capturedHost == nil {
		t.Fatal("RuntimeHost was not passed to Start")
	}

	caps, err := capturedHost.ListCapabilities(rpcCtx)
	if err != nil {
		t.Fatalf("ListCapabilities via SDK host: %v", err)
	}
	if len(caps) != 1 || caps[0].Provider != "alpha" || caps[0].Operation != "read" {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}

	if _, err := client.Stop(rpcCtx, &emptypb.Empty{}); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

type capturingRuntime struct {
	host *gestalt.RuntimeHost
}

func (r *capturingRuntime) Start(_ context.Context, _ string, _ map[string]any, _ []gestalt.Capability, host gestalt.RuntimeHost) error {
	*r.host = host
	return nil
}

func (r *capturingRuntime) Stop(context.Context) error {
	return nil
}

