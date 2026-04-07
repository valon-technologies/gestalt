package gestalt_test

import (
	"context"
	"net/http"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type stubProvider struct{}

type stubInput struct{}

type stubOutput struct {
	Operation string `json:"operation"`
}

var stubRouter = gestalt.MustRouter(
	gestalt.Register(
		gestalt.Operation[stubInput, stubOutput]{
			ID:     "test_op",
			Method: http.MethodPost,
		},
		(*stubProvider).testOp,
	),
)

var startableStubRouter = gestalt.MustRouter(
	gestalt.Register(
		gestalt.Operation[stubInput, stubOutput]{
			ID:     "test_op",
			Method: http.MethodPost,
		},
		(*startableStubProvider).testOp,
	),
)

var sessionCatalogStubRouter = gestalt.MustRouter(
	gestalt.Register(
		gestalt.Operation[stubInput, stubOutput]{
			ID:     "test_op",
			Method: http.MethodPost,
		},
		(*sessionCatalogStubProvider).testOp,
	),
)

func (p *stubProvider) Configure(_ context.Context, _ string, _ map[string]any) error {
	return nil
}

func (p *stubProvider) testOp(_ context.Context, _ stubInput, _ gestalt.Request) (gestalt.Response[stubOutput], error) {
	return gestalt.OK(stubOutput{Operation: "test_op"}), nil
}

type startableStubProvider struct {
	stubProvider
	name   string
	config map[string]any
}

func (p *startableStubProvider) Configure(_ context.Context, name string, config map[string]any) error {
	p.name = name
	p.config = config
	return nil
}

type sessionCatalogStubProvider struct {
	stubProvider
	sessionCatalog *gestalt.Catalog
}

func (p *sessionCatalogStubProvider) CatalogForRequest(_ context.Context, _ string) (*gestalt.Catalog, error) {
	return p.sessionCatalog, nil
}

func TestProviderServerGetMetadata(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, &stubProvider{}, stubRouter)
	meta, err := client.GetMetadata(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetSupportsSessionCatalog() {
		t.Fatal("SupportsSessionCatalog = true, want false")
	}
}

func TestProviderServerGetMetadata_SessionCatalogCapability(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, &sessionCatalogStubProvider{
		sessionCatalog: &gestalt.Catalog{
			Name: "test-provider",
			Operations: []gestalt.CatalogOperation{
				{ID: "session_op", Method: http.MethodGet},
			},
		},
	}, sessionCatalogStubRouter)
	meta, err := client.GetMetadata(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if !meta.GetSupportsSessionCatalog() {
		t.Fatal("SupportsSessionCatalog = false, want true")
	}
}

func TestProviderServerGetSessionCatalog(t *testing.T) {
	t.Parallel()

	prov := &sessionCatalogStubProvider{
		sessionCatalog: &gestalt.Catalog{
			Name: "test-provider",
			Operations: []gestalt.CatalogOperation{
				{ID: "session_op", Method: http.MethodPost},
			},
		},
	}

	client := newProviderPluginClient(t, prov, sessionCatalogStubRouter)
	resp, err := client.GetSessionCatalog(context.Background(), &proto.GetSessionCatalogRequest{Token: "tok"})
	if err != nil {
		t.Fatalf("GetSessionCatalog: %v", err)
	}
	if resp.GetCatalogJson() == "" {
		t.Fatal("expected session catalog json")
	}
}

func TestProviderServerExecute(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, &stubProvider{}, stubRouter)
	ctx := context.Background()

	params, _ := structpb.NewStruct(map[string]any{"key": "value"})
	resp, err := client.Execute(ctx, &proto.ExecuteRequest{
		Operation: "test_op",
		Params:    params,
		Token:     "tok",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.GetStatus() != 200 {
		t.Errorf("Status = %d, want 200", resp.GetStatus())
	}
	if resp.GetBody() != `{"operation":"test_op"}` {
		t.Errorf("Body = %q, want %q", resp.GetBody(), `{"operation":"test_op"}`)
	}
}

func TestProviderServerStartProvider(t *testing.T) {
	t.Parallel()

	prov := &startableStubProvider{}
	client := newProviderPluginClient(t, prov, startableStubRouter)
	ctx := context.Background()

	cfg, _ := structpb.NewStruct(map[string]any{"key": "val"})
	resp, err := client.StartProvider(ctx, &proto.StartProviderRequest{
		Name:            "my-instance",
		Config:          cfg,
		ProtocolVersion: proto.CurrentProtocolVersion,
	})
	if err != nil {
		t.Fatalf("StartProvider: %v", err)
	}
	if resp.GetProtocolVersion() != proto.CurrentProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", resp.GetProtocolVersion(), proto.CurrentProtocolVersion)
	}
	if prov.name != "my-instance" {
		t.Errorf("name = %q, want %q", prov.name, "my-instance")
	}
	if prov.config["key"] != "val" {
		t.Errorf("config[key] = %v, want %q", prov.config["key"], "val")
	}
}

func TestProviderServerUnimplementedRPCs(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, &stubProvider{}, stubRouter)
	ctx := context.Background()

	_, err := client.GetSessionCatalog(ctx, &proto.GetSessionCatalogRequest{Token: "t"})
	if err == nil {
		t.Error("GetSessionCatalog should return UNIMPLEMENTED")
	}

	_, err = client.PostConnect(ctx, &proto.PostConnectRequest{})
	if err == nil {
		t.Error("PostConnect should return UNIMPLEMENTED")
	}
}
