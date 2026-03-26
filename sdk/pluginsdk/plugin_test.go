package pluginsdk_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type functionalProvider struct {
	startName   string
	startMode   string
	startConfig map[string]any
}

func (p *functionalProvider) Name() string        { return "fixture" }
func (p *functionalProvider) DisplayName() string { return "Fixture Provider" }
func (p *functionalProvider) Description() string { return "Generic provider fixture" }
func (p *functionalProvider) ConnectionMode() pluginsdk.ConnectionMode {
	return pluginsdk.ConnectionModeEither
}

func (p *functionalProvider) ListOperations() []pluginsdk.Operation {
	return []pluginsdk.Operation{
		{
			Name:        "inspect",
			Description: "Return request details",
			Method:      "POST",
			Parameters: []pluginsdk.Parameter{
				{Name: "message", Type: "string", Description: "Message to echo", Required: true},
			},
		},
	}
}

func (p *functionalProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*pluginsdk.OperationResult, error) {
	body, err := json.Marshal(map[string]any{
		"operation":         operation,
		"token":             token,
		"params":            params,
		"connection_params": pluginsdk.ConnectionParams(ctx),
		"start_name":        p.startName,
		"start_mode":        p.startMode,
		"greeting":          p.startConfig["greeting"],
	})
	if err != nil {
		return nil, err
	}
	return &pluginsdk.OperationResult{Status: 207, Body: string(body)}, nil
}

func (p *functionalProvider) Start(_ context.Context, name string, config map[string]any, mode string) error {
	p.startName = name
	p.startMode = mode
	p.startConfig = config
	return nil
}

func (p *functionalProvider) ConfigSchemaJSON() string {
	return `{"type":"object","properties":{"greeting":{"type":"string"}}}`
}

func (p *functionalProvider) ProtocolVersionRange() (int32, int32) {
	return pluginapiv1.CurrentProtocolVersion, pluginapiv1.CurrentProtocolVersion
}

func (p *functionalProvider) ConnectionParamDefs() map[string]pluginsdk.ConnectionParamDef {
	return map[string]pluginsdk.ConnectionParamDef{
		"workspace": {Required: true, Description: "Workspace identifier"},
	}
}

type runtimeHostFixture struct {
	pluginapiv1.UnimplementedRuntimeHostServer
	invokeCount int
}

func (h *runtimeHostFixture) ListCapabilities(context.Context, *emptypb.Empty) (*pluginapiv1.ListCapabilitiesResponse, error) {
	return &pluginapiv1.ListCapabilitiesResponse{
		Capabilities: []*pluginapiv1.Capability{
			{Provider: "fixture", Operation: "inspect"},
		},
	}, nil
}

func (h *runtimeHostFixture) Invoke(context.Context, *pluginapiv1.InvokeRequest) (*pluginapiv1.OperationResult, error) {
	h.invokeCount++
	return &pluginapiv1.OperationResult{Status: 299, Body: "host-response"}, nil
}

type hostDialingRuntime struct {
	pluginapiv1.UnimplementedRuntimePluginServer
	startReq   *pluginapiv1.StartRuntimeRequest
	hostCaps   int
	hostStatus int32
	hostBody   string
	stopCalls  int
	hostConn   *grpc.ClientConn
}

func (r *hostDialingRuntime) Start(ctx context.Context, req *pluginapiv1.StartRuntimeRequest) (*emptypb.Empty, error) {
	conn, host, err := pluginsdk.DialRuntimeHost(ctx)
	if err != nil {
		return nil, err
	}
	r.hostConn = conn
	r.startReq = req

	caps, err := host.ListCapabilities(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	r.hostCaps = len(caps.GetCapabilities())

	resp, err := host.Invoke(ctx, &pluginapiv1.InvokeRequest{
		Provider:  "fixture",
		Operation: "inspect",
	})
	if err != nil {
		return nil, err
	}
	r.hostStatus = resp.GetStatus()
	r.hostBody = resp.GetBody()

	return &emptypb.Empty{}, nil
}

func (r *hostDialingRuntime) Stop(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	r.stopCalls++
	if r.hostConn != nil {
		_ = r.hostConn.Close()
		r.hostConn = nil
	}
	return &emptypb.Empty{}, nil
}

func TestServeProviderOverUnixSocket(t *testing.T) {
	provider := &functionalProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socketDir := newShortSocketDir(t)
	socket := filepath.Join(socketDir, "provider.sock")
	t.Setenv(pluginapiv1.EnvPluginSocket, socket)

	errCh := make(chan error, 1)
	go func() {
		errCh <- pluginsdk.ServeProvider(ctx, provider)
	}()

	client := newProviderClient(t, socket)

	meta, err := client.GetMetadata(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetName() != "fixture" || meta.GetDisplayName() != "Fixture Provider" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if meta.GetConfigSchemaJson() == "" {
		t.Fatal("expected config schema in metadata")
	}
	if meta.GetConnectionParams()["workspace"].GetDescription() != "Workspace identifier" {
		t.Fatalf("unexpected connection params: %+v", meta.GetConnectionParams())
	}
	if meta.GetMinProtocolVersion() != pluginapiv1.CurrentProtocolVersion || meta.GetMaxProtocolVersion() != pluginapiv1.CurrentProtocolVersion {
		t.Fatalf("unexpected protocol versions: min=%d max=%d", meta.GetMinProtocolVersion(), meta.GetMaxProtocolVersion())
	}

	cfg, err := structpb.NewStruct(map[string]any{"greeting": "Hello"})
	if err != nil {
		t.Fatalf("NewStruct(config): %v", err)
	}
	startResp, err := client.StartProvider(context.Background(), &pluginapiv1.StartProviderRequest{
		Name:            "fixture-instance",
		Config:          cfg,
		Mode:            pluginapiv1.PluginMode_PLUGIN_MODE_OVERLAY,
		ProtocolVersion: pluginapiv1.CurrentProtocolVersion,
	})
	if err != nil {
		t.Fatalf("StartProvider: %v", err)
	}
	if startResp.GetProtocolVersion() != pluginapiv1.CurrentProtocolVersion {
		t.Fatalf("unexpected protocol version: %d", startResp.GetProtocolVersion())
	}

	ops, err := client.ListOperations(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("ListOperations: %v", err)
	}
	if len(ops.GetOperations()) != 1 || ops.GetOperations()[0].GetName() != "inspect" {
		t.Fatalf("unexpected operations: %+v", ops.GetOperations())
	}

	params, err := structpb.NewStruct(map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("NewStruct(params): %v", err)
	}
	resp, err := client.Execute(context.Background(), &pluginapiv1.ExecuteRequest{
		Operation:        "inspect",
		Params:           params,
		Token:            "token-123",
		ConnectionParams: map[string]string{"workspace": "alpha"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.GetStatus() != 207 {
		t.Fatalf("unexpected status: %d", resp.GetStatus())
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(resp.GetBody()), &body); err != nil {
		t.Fatalf("Unmarshal execute body: %v", err)
	}
	if body["start_name"] != "fixture-instance" || body["start_mode"] != pluginsdk.PluginModeOverlay {
		t.Fatalf("unexpected start details in execute body: %+v", body)
	}
	if body["greeting"] != "Hello" || body["token"] != "token-123" {
		t.Fatalf("unexpected execute body: %+v", body)
	}
	connParams, ok := body["connection_params"].(map[string]any)
	if !ok || connParams["workspace"] != "alpha" {
		t.Fatalf("unexpected connection params in body: %+v", body["connection_params"])
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ServeProvider returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServeProvider did not stop")
	}
}

func TestServeRuntimeAndDialRuntimeHostOverUnixSockets(t *testing.T) {
	socketDir := newShortSocketDir(t)
	pluginSocket := filepath.Join(socketDir, "runtime.sock")
	hostSocket := filepath.Join(socketDir, "host.sock")
	t.Setenv(pluginapiv1.EnvPluginSocket, pluginSocket)
	t.Setenv(pluginapiv1.EnvRuntimeHostSocket, hostSocket)

	hostListener, err := net.Listen("unix", hostSocket)
	if err != nil {
		t.Fatalf("Listen(host): %v", err)
	}
	defer func() { _ = hostListener.Close() }()

	hostFixture := &runtimeHostFixture{}
	hostServer := grpc.NewServer()
	pluginapiv1.RegisterRuntimeHostServer(hostServer, hostFixture)
	go func() {
		_ = hostServer.Serve(hostListener)
	}()
	defer hostServer.Stop()

	runtimeServer := &hostDialingRuntime{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- pluginsdk.ServeRuntime(ctx, runtimeServer)
	}()

	client := newRuntimeClient(t, pluginSocket)
	config, err := structpb.NewStruct(map[string]any{"mode": "verification"})
	if err != nil {
		t.Fatalf("NewStruct(config): %v", err)
	}
	_, err = client.Start(context.Background(), &pluginapiv1.StartRuntimeRequest{
		Name:   "fixture-runtime",
		Config: config,
		InitialCapabilities: []*pluginapiv1.Capability{
			{Provider: "seed", Operation: "boot"},
		},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if runtimeServer.startReq == nil || runtimeServer.startReq.GetName() != "fixture-runtime" {
		t.Fatalf("unexpected start request: %+v", runtimeServer.startReq)
	}
	if len(runtimeServer.startReq.GetInitialCapabilities()) != 1 || runtimeServer.startReq.GetInitialCapabilities()[0].GetProvider() != "seed" {
		t.Fatalf("unexpected initial capabilities: %+v", runtimeServer.startReq.GetInitialCapabilities())
	}
	if runtimeServer.hostCaps != 1 {
		t.Fatalf("expected one host capability, got %d", runtimeServer.hostCaps)
	}
	if runtimeServer.hostStatus != 299 || runtimeServer.hostBody != "host-response" {
		t.Fatalf("unexpected host response: status=%d body=%q", runtimeServer.hostStatus, runtimeServer.hostBody)
	}
	if hostFixture.invokeCount != 1 {
		t.Fatalf("expected one host invocation, got %d", hostFixture.invokeCount)
	}

	if _, err := client.Stop(context.Background(), &emptypb.Empty{}); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if runtimeServer.stopCalls != 1 {
		t.Fatalf("expected one stop call, got %d", runtimeServer.stopCalls)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ServeRuntime returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServeRuntime did not stop")
	}
}

func newProviderClient(t *testing.T, socket string) pluginapiv1.ProviderPluginClient {
	t.Helper()
	conn := newUnixConn(t, socket)
	t.Cleanup(func() { _ = conn.Close() })
	return pluginapiv1.NewProviderPluginClient(conn)
}

func newRuntimeClient(t *testing.T, socket string) pluginapiv1.RuntimePluginClient {
	t.Helper()
	conn := newUnixConn(t, socket)
	t.Cleanup(func() { _ = conn.Close() })
	return pluginapiv1.NewRuntimePluginClient(conn)
}

func newUnixConn(t *testing.T, socket string) *grpc.ClientConn {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket %q was not created", socket)
		}
		time.Sleep(25 * time.Millisecond)
	}

	conn, err := grpc.NewClient(
		"passthrough:///"+socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", addr)
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	conn.Connect()
	return conn
}

func newShortSocketDir(t *testing.T) string {
	t.Helper()

	base := "/tmp"
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		base = os.TempDir()
	}
	dir, err := os.MkdirTemp(base, "psdk-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
