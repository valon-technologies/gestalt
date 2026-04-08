package gestalt_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
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

type decodeInput struct {
	Count int `json:"count"`
}

type decodeOutput struct {
	Count int `json:"count"`
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

func (p *stubProvider) decodeOp(_ context.Context, input decodeInput, _ gestalt.Request) (gestalt.Response[decodeOutput], error) {
	return gestalt.OK(decodeOutput{Count: input.Count}), nil
}

func (p *stubProvider) errorOp(_ context.Context, _ stubInput, _ gestalt.Request) (gestalt.Response[stubOutput], error) {
	return gestalt.Response[stubOutput]{}, errors.New("boom")
}

func (p *stubProvider) panicOp(_ context.Context, _ stubInput, _ gestalt.Request) (gestalt.Response[stubOutput], error) {
	panic("boom")
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

	t.Run("plain provider", func(t *testing.T) {
		client := newProviderPluginClient(t, &stubProvider{}, stubRouter)
		meta, err := client.GetMetadata(context.Background(), &emptypb.Empty{})
		if err != nil {
			t.Fatalf("GetMetadata: %v", err)
		}
		if meta.GetSupportsSessionCatalog() {
			t.Fatal("SupportsSessionCatalog = true, want false")
		}
	})

	t.Run("session catalog provider", func(t *testing.T) {
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
	})
}

func TestProviderServerGetSessionCatalog(t *testing.T) {
	t.Parallel()

	t.Run("supported", func(t *testing.T) {
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
	})

	t.Run("unsupported", func(t *testing.T) {
		client := newProviderPluginClient(t, &stubProvider{}, stubRouter)
		_, err := client.GetSessionCatalog(context.Background(), &proto.GetSessionCatalogRequest{Token: "t"})
		if err == nil {
			t.Fatal("GetSessionCatalog should return error for unsupported provider")
		}
	})
}

func TestProviderServerExecute(t *testing.T) {
	t.Parallel()

	decodeRouter := gestalt.MustRouter(
		gestalt.Register(
			gestalt.Operation[decodeInput, decodeOutput]{
				ID:     "decode_op",
				Method: http.MethodPost,
			},
			(*stubProvider).decodeOp,
		),
	)
	errorRouter := gestalt.MustRouter(
		gestalt.Register(
			gestalt.Operation[stubInput, stubOutput]{
				ID:     "error_op",
				Method: http.MethodPost,
			},
			(*stubProvider).errorOp,
		),
	)
	panicRouter := gestalt.MustRouter(
		gestalt.Register(
			gestalt.Operation[stubInput, stubOutput]{
				ID:     "panic_op",
				Method: http.MethodPost,
			},
			(*stubProvider).panicOp,
		),
	)

	tests := []struct {
		name            string
		router          *gestalt.Router[stubProvider]
		request         *proto.ExecuteRequest
		wantStatus      int32
		wantBody        string
		wantBodyContain []string
	}{
		{
			name:       "success",
			router:     stubRouter,
			wantStatus: http.StatusOK,
			wantBody:   `{"operation":"test_op"}`,
			request: &proto.ExecuteRequest{
				Operation: "test_op",
				Params: func() *structpb.Struct {
					params, _ := structpb.NewStruct(map[string]any{"key": "value"})
					return params
				}(),
				Token: "tok",
			},
		},
		{
			name:       "decode error",
			router:     decodeRouter,
			wantStatus: http.StatusBadRequest,
			wantBodyContain: []string{
				"decode params for",
				"decode_op",
			},
			request: &proto.ExecuteRequest{
				Operation: "decode_op",
				Params: func() *structpb.Struct {
					params, _ := structpb.NewStruct(map[string]any{"count": "oops"})
					return params
				}(),
			},
		},
		{
			name:       "handler error",
			router:     errorRouter,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"boom"}`,
			request: &proto.ExecuteRequest{
				Operation: "error_op",
			},
		},
		{
			name:       "panic",
			router:     panicRouter,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"boom"}`,
			request: &proto.ExecuteRequest{
				Operation: "panic_op",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newProviderPluginClient(t, &stubProvider{}, tt.router)

			resp, err := client.Execute(context.Background(), tt.request)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if resp.GetStatus() != tt.wantStatus {
				t.Fatalf("Status = %d, want %d", resp.GetStatus(), tt.wantStatus)
			}
			if tt.wantBody != "" && resp.GetBody() != tt.wantBody {
				t.Fatalf("Body = %q, want %q", resp.GetBody(), tt.wantBody)
			}
			for _, want := range tt.wantBodyContain {
				if !strings.Contains(resp.GetBody(), want) {
					t.Fatalf("Body = %q, want substring %q", resp.GetBody(), want)
				}
			}
		})
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

