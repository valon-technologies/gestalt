package pluginsdk_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginsdk/proto/v1"
	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type stubRuntime struct {
	started      int
	stopped      int
	lastName     string
	lastInitCaps []pluginsdk.Capability
}

func (s *stubRuntime) Start(_ context.Context, name string, _ map[string]any, caps []pluginsdk.Capability, _ pluginsdk.RuntimeHost) error {
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

func newRuntimeTestEnv(t *testing.T, rt pluginsdk.Runtime) pluginapiv1.RuntimePluginClient {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "sdk-rt-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	pluginSocket := filepath.Join(dir, "rt.sock")
	hostSocket := filepath.Join(dir, "host.sock")
	t.Setenv(pluginapiv1.EnvPluginSocket, pluginSocket)
	t.Setenv(pluginapiv1.EnvRuntimeHostSocket, hostSocket)

	hostLis, err := net.Listen("unix", hostSocket)
	if err != nil {
		t.Fatalf("net.Listen host: %v", err)
	}
	t.Cleanup(func() { _ = hostLis.Close() })

	hostSrv := grpc.NewServer()
	pluginapiv1.RegisterRuntimeHostServer(hostSrv, &stubRuntimeHost{})
	go func() { _ = hostSrv.Serve(hostLis) }()
	t.Cleanup(hostSrv.Stop)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- pluginsdk.ServeRuntime(ctx, rt)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
	})

	conn := newUnixConn(t, pluginSocket)
	return pluginapiv1.NewRuntimePluginClient(conn)
}

func TestServeRuntimeRoundTrip(t *testing.T) {
	rt := &stubRuntime{}
	client := newRuntimeTestEnv(t, rt)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), time.Second)
	defer rpcCancel()

	startReq := &pluginapiv1.StartRuntimeRequest{
		Name: "echo",
		InitialCapabilities: []*pluginapiv1.Capability{
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
	var capturedHost pluginsdk.RuntimeHost
	capturingRT := &capturingRuntime{host: &capturedHost}
	client := newRuntimeTestEnv(t, capturingRT)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rpcCancel()

	if _, err := client.Start(rpcCtx, &pluginapiv1.StartRuntimeRequest{Name: "test-rt"}, grpc.WaitForReady(true)); err != nil {
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
	host *pluginsdk.RuntimeHost
}

func (r *capturingRuntime) Start(_ context.Context, _ string, _ map[string]any, _ []pluginsdk.Capability, host pluginsdk.RuntimeHost) error {
	*r.host = host
	return nil
}

func (r *capturingRuntime) Stop(context.Context) error {
	return nil
}

type stubProviderHost struct {
	pluginapiv1.UnimplementedProviderHostServer
}

const (
	stubStatusCode  = 418
	stubHeaderKey   = "X-Stub"
	stubHeaderValue = "dummy"
	stubBodyContent = "response-body-bytes"
)

func (s *stubProviderHost) ProxyHTTP(_ context.Context, req *pluginapiv1.ProxyHTTPRequest) (*pluginapiv1.ProxyHTTPResponse, error) {
	return &pluginapiv1.ProxyHTTPResponse{
		StatusCode: int32(stubStatusCode),
		Headers:    map[string]string{stubHeaderKey: stubHeaderValue},
		Body:       []byte(stubBodyContent),
	}, nil
}

func TestDialProviderHostIntegration(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sdk-ph-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	socket := filepath.Join(dir, "ph.sock")

	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	srv := grpc.NewServer()
	pluginapiv1.RegisterProviderHostServer(srv, &stubProviderHost{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	t.Setenv(pluginapiv1.EnvProviderHostSocket, socket)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	host, err := pluginsdk.DialProviderHost(ctx)
	if err != nil {
		t.Fatalf("DialProviderHost: %v", err)
	}
	defer func() { _ = host.Close() }()

	resp, err := host.ProxyHTTP(ctx, "inv-123", http.MethodGet, "https://dummy.example.com/items", nil, nil)
	if err != nil {
		t.Fatalf("ProxyHTTP: %v", err)
	}
	if resp.StatusCode != stubStatusCode {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, stubStatusCode)
	}
	if resp.Headers[stubHeaderKey] != stubHeaderValue {
		t.Errorf("Headers[%q] = %q, want %q", stubHeaderKey, resp.Headers[stubHeaderKey], stubHeaderValue)
	}
	if string(resp.Body) != stubBodyContent {
		t.Errorf("Body = %q, want %q", string(resp.Body), stubBodyContent)
	}
}
