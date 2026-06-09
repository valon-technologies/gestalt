package gestalt_test

import (
	"context"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type stubTestProvider struct {
	closeTracker
	configured []configCall
}

func (p *stubTestProvider) Configure(_ context.Context, name string, config map[string]any) error {
	p.configured = append(p.configured, configCall{name: name, config: config})
	return nil
}

func (p *stubTestProvider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindTest,
		Name:        "stub-test",
		DisplayName: "Stub Test",
		Version:     "1.0",
	}
}

func (p *stubTestProvider) HelloWorld(context.Context, *gestalt.HelloWorldRequest) (*gestalt.HelloWorldResponse, error) {
	return &gestalt.HelloWorldResponse{Message: "HelloWorld"}, nil
}

func TestTestProviderRoundTrip(t *testing.T) {
	socket := newSocketPath(t, "test-provider.sock")
	t.Setenv(proto.EnvProviderSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	provider := &stubTestProvider{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeTestProvider(ctx, provider)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	runtimeClient := proto.NewProviderLifecycleClient(conn)
	testClient := proto.NewTestClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()

	meta, err := runtimeClient.GetProviderIdentity(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetProviderIdentity: %v", err)
	}
	if meta.GetKind() != proto.ProviderKind_PROVIDER_KIND_TEST {
		t.Fatalf("kind = %v, want TEST", meta.GetKind())
	}
	if meta.GetName() != "stub-test" {
		t.Fatalf("name = %q, want %q", meta.GetName(), "stub-test")
	}

	cfg, _ := structpb.NewStruct(map[string]any{"enabled": true})
	configuredResp, err := runtimeClient.ConfigureProvider(rpcCtx, &proto.ConfigureProviderRequest{
		Name:            "my-test",
		Config:          cfg,
		ProtocolVersion: proto.CurrentProtocolVersion,
	})
	if err != nil {
		t.Fatalf("ConfigureProvider: %v", err)
	}
	if configuredResp.GetProtocolVersion() != proto.CurrentProtocolVersion {
		t.Fatalf("configured protocol_version = %d, want %d", configuredResp.GetProtocolVersion(), proto.CurrentProtocolVersion)
	}
	if len(provider.configured) != 1 {
		t.Fatalf("configured calls = %d, want 1", len(provider.configured))
	}
	if provider.configured[0].name != "my-test" {
		t.Fatalf("configured name = %q, want %q", provider.configured[0].name, "my-test")
	}
	if provider.configured[0].config["enabled"] != true {
		t.Fatalf("configured config[enabled] = %v, want true", provider.configured[0].config["enabled"])
	}

	resp, err := testClient.HelloWorld(rpcCtx, &proto.HelloWorldRequest{})
	if err != nil {
		t.Fatalf("HelloWorld: %v", err)
	}
	if resp.GetMessage() != "HelloWorld" {
		t.Fatalf("message = %q, want %q", resp.GetMessage(), "HelloWorld")
	}
}
