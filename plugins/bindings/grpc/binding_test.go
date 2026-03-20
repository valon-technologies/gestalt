package grpc_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/testutil"
	grpcbinding "github.com/valon-technologies/gestalt/plugins/bindings/grpc"
	pb "github.com/valon-technologies/gestalt/proto/toolshed/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"
)

type stubCapabilityLister struct {
	caps []core.Capability
}

func (s *stubCapabilityLister) ListCapabilities() []core.Capability { return s.caps }

type stubProviderLister struct {
	providers []core.ProviderInfo
}

func (s *stubProviderLister) ListProviderInfos() []core.ProviderInfo { return s.providers }

func freePort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	_ = lis.Close()
	return port
}

func startBinding(t *testing.T, invoker invocation.Invoker, capLister invocation.CapabilityLister, provLister invocation.ProviderLister) (pb.ToolshedServiceClient, int) {
	t.Helper()
	port := freePort(t)

	cfgYAML := fmt.Sprintf("port: %d", port)
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}

	def := config.BindingDef{Type: "grpc", Config: *node.Content[0]}
	binding, err := grpcbinding.Factory(context.Background(), "test-grpc", def, bootstrap.BindingDeps{
		Invoker:          invoker,
		CapabilityLister: capLister,
		ProviderLister:   provLister,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := binding.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = binding.Close() })

	conn, err := grpc.NewClient(
		fmt.Sprintf("127.0.0.1:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return pb.NewToolshedServiceClient(conn), port
}

func TestGRPCBindingKind(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	cfgYAML := fmt.Sprintf("port: %d", port)
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}
	def := config.BindingDef{Type: "grpc", Config: *node.Content[0]}
	binding, err := grpcbinding.Factory(context.Background(), "test", def, bootstrap.BindingDeps{
		Invoker: &testutil.StubInvoker{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Kind() != core.BindingSurface {
		t.Fatalf("expected BindingSurface, got %d", binding.Kind())
	}
}

func TestGRPCBindingNoRoutes(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	cfgYAML := fmt.Sprintf("port: %d", port)
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}
	def := config.BindingDef{Type: "grpc", Config: *node.Content[0]}
	binding, err := grpcbinding.Factory(context.Background(), "test", def, bootstrap.BindingDeps{
		Invoker: &testutil.StubInvoker{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if routes := binding.Routes(); routes != nil {
		t.Fatalf("expected nil routes, got %v", routes)
	}
}

func TestGRPCListProviders(t *testing.T) {
	t.Parallel()

	provLister := &stubProviderLister{
		providers: []core.ProviderInfo{
			{Name: "slack", DisplayName: "Slack", Description: "Slack messaging", ConnectionMode: core.ConnectionModeUser},
			{Name: "github", DisplayName: "GitHub", Description: "GitHub API", ConnectionMode: core.ConnectionModeIdentity},
		},
	}

	client, _ := startBinding(t, &testutil.StubInvoker{}, nil, provLister)

	resp, err := client.ListProviders(context.Background(), &pb.ListProvidersRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(resp.Providers))
	}

	if resp.Providers[0].Name != "slack" {
		t.Errorf("expected slack, got %q", resp.Providers[0].Name)
	}
	if resp.Providers[0].DisplayName != "Slack" {
		t.Errorf("expected Slack, got %q", resp.Providers[0].DisplayName)
	}
	if resp.Providers[0].ConnectionMode != "user" {
		t.Errorf("expected user, got %q", resp.Providers[0].ConnectionMode)
	}

	if resp.Providers[1].Name != "github" {
		t.Errorf("expected github, got %q", resp.Providers[1].Name)
	}
}

func TestGRPCListCapabilities(t *testing.T) {
	t.Parallel()

	capLister := &stubCapabilityLister{
		caps: []core.Capability{
			{
				Provider:    "slack",
				Operation:   "send_message",
				Description: "Send a Slack message",
				Parameters: []core.Parameter{
					{Name: "channel", Type: "string", Description: "Channel ID", Required: true},
					{Name: "text", Type: "string", Description: "Message text", Required: true},
				},
			},
			{
				Provider:    "github",
				Operation:   "create_issue",
				Description: "Create a GitHub issue",
			},
		},
	}

	client, _ := startBinding(t, &testutil.StubInvoker{}, capLister, nil)

	resp, err := client.ListCapabilities(context.Background(), &pb.ListCapabilitiesRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Capabilities) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(resp.Capabilities))
	}

	slack := resp.Capabilities[0]
	if slack.Provider != "slack" || slack.Operation != "send_message" {
		t.Errorf("unexpected first capability: %v", slack)
	}
	if len(slack.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(slack.Parameters))
	}
	if slack.Parameters[0].Name != "channel" || !slack.Parameters[0].Required {
		t.Errorf("unexpected parameter: %v", slack.Parameters[0])
	}
}

func TestGRPCListCapabilities_FilterByProvider(t *testing.T) {
	t.Parallel()

	capLister := &stubCapabilityLister{
		caps: []core.Capability{
			{Provider: "slack", Operation: "send_message"},
			{Provider: "slack", Operation: "list_channels"},
			{Provider: "github", Operation: "create_issue"},
		},
	}

	client, _ := startBinding(t, &testutil.StubInvoker{}, capLister, nil)

	resp, err := client.ListCapabilities(context.Background(), &pb.ListCapabilitiesRequest{
		Providers: []string{"slack"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Capabilities) != 2 {
		t.Fatalf("expected 2 slack capabilities, got %d", len(resp.Capabilities))
	}
	for _, cap := range resp.Capabilities {
		if cap.Provider != "slack" {
			t.Errorf("expected only slack capabilities, got provider %q", cap.Provider)
		}
	}
}

func TestGRPCInvoke(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{
			Status: 200,
			Body:   `{"ok":true}`,
		},
	}

	client, _ := startBinding(t, invoker, nil, nil)

	params, _ := structpb.NewStruct(map[string]any{
		"channel": "C123",
		"text":    "hello",
	})

	resp, err := client.Invoke(context.Background(), &pb.InvokeRequest{
		Provider:  "slack",
		Operation: "send_message",
		Params:    params,
		UserId:    "user-42",
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if resp.Body != `{"ok":true}` {
		t.Errorf("expected body, got %q", resp.Body)
	}

	if !invoker.Invoked {
		t.Fatal("expected invoker to be called")
	}
	if invoker.Provider != "slack" {
		t.Errorf("expected provider slack, got %q", invoker.Provider)
	}
	if invoker.Operation != "send_message" {
		t.Errorf("expected operation send_message, got %q", invoker.Operation)
	}
	if invoker.Params["channel"] != "C123" {
		t.Errorf("expected channel C123, got %v", invoker.Params["channel"])
	}
}

func TestGRPCInvoke_PrincipalFromRequestField(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: 200, Body: `{}`},
	}

	client, _ := startBinding(t, invoker, nil, nil)

	_, err := client.Invoke(context.Background(), &pb.InvokeRequest{
		Provider:  "echo",
		Operation: "echo",
		UserId:    "user-from-field",
	})
	if err != nil {
		t.Fatal(err)
	}

	if invoker.LastP == nil {
		t.Fatal("expected principal")
	}
	if invoker.LastP.UserID != "user-from-field" {
		t.Errorf("expected user-from-field, got %q", invoker.LastP.UserID)
	}

	p := principal.FromContext(invoker.LastCtx)
	if p == nil || p.UserID != "user-from-field" {
		t.Errorf("expected principal in context with user-from-field")
	}
}

func TestGRPCInvoke_PrincipalFromMetadata(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: 200, Body: `{}`},
	}

	client, _ := startBinding(t, invoker, nil, nil)

	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "user-from-meta")
	_, err := client.Invoke(ctx, &pb.InvokeRequest{
		Provider:  "echo",
		Operation: "echo",
	})
	if err != nil {
		t.Fatal(err)
	}

	if invoker.LastP == nil {
		t.Fatal("expected principal")
	}
	if invoker.LastP.UserID != "user-from-meta" {
		t.Errorf("expected user-from-meta, got %q", invoker.LastP.UserID)
	}
}

func TestGRPCInvoke_ProviderNotFound(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Err: fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, "nonexistent"),
	}

	client, _ := startBinding(t, invoker, nil, nil)

	_, err := client.Invoke(context.Background(), &pb.InvokeRequest{
		Provider:  "nonexistent",
		Operation: "op",
		UserId:    "u",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", st.Code())
	}
}

func TestGRPCInvoke_MissingProvider(t *testing.T) {
	t.Parallel()

	client, _ := startBinding(t, &testutil.StubInvoker{}, nil, nil)

	_, err := client.Invoke(context.Background(), &pb.InvokeRequest{
		Operation: "op",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestGRPCInvoke_MissingOperation(t *testing.T) {
	t.Parallel()

	client, _ := startBinding(t, &testutil.StubInvoker{}, nil, nil)

	_, err := client.Invoke(context.Background(), &pb.InvokeRequest{
		Provider: "slack",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestGRPCFactory_DefaultPort(t *testing.T) {
	t.Parallel()

	cfgYAML := "{}"
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}
	def := config.BindingDef{Type: "grpc", Config: *node.Content[0]}
	binding, err := grpcbinding.Factory(context.Background(), "test", def, bootstrap.BindingDeps{
		Invoker: &testutil.StubInvoker{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Name() != "test" {
		t.Errorf("expected name test, got %q", binding.Name())
	}
}

func TestGRPCFactory_MissingInvoker(t *testing.T) {
	t.Parallel()

	cfgYAML := "port: 50051"
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}
	def := config.BindingDef{Type: "grpc", Config: *node.Content[0]}
	_, err := grpcbinding.Factory(context.Background(), "test", def, bootstrap.BindingDeps{})
	if err == nil {
		t.Fatal("expected error for missing invoker")
	}
}

func TestGRPCFactory_InvalidPort(t *testing.T) {
	t.Parallel()

	cfgYAML := "port: 99999"
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}
	def := config.BindingDef{Type: "grpc", Config: *node.Content[0]}
	_, err := grpcbinding.Factory(context.Background(), "test", def, bootstrap.BindingDeps{
		Invoker: &testutil.StubInvoker{},
	})
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}
