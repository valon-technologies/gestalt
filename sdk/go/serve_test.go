package gestalt_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	protoutil "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type stubProvider struct{}

type stubInput struct{}

type stubOutput struct {
	Operation           string `json:"operation"`
	SubjectID           string `json:"subject_id"`
	SubjectKind         string `json:"subject_kind"`
	CredentialMode      string `json:"credential_mode"`
	CredentialSubjectID string `json:"credential_subject_id"`
	AccessPolicy        string `json:"access_policy"`
	AccessRole          string `json:"access_role"`
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

func (p *stubProvider) testOp(_ context.Context, _ stubInput, req gestalt.Request) (gestalt.Response[stubOutput], error) {
	return gestalt.OK(stubOutput{
		Operation:           "test_op",
		SubjectID:           req.Subject.ID,
		SubjectKind:         req.Subject.Kind,
		CredentialMode:      req.Credential.Mode,
		CredentialSubjectID: req.Credential.SubjectID,
		AccessPolicy:        req.Access.Policy,
		AccessRole:          req.Access.Role,
	}), nil
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
	sessionCatalog *proto.Catalog
}

type panicHTTPSubjectProvider struct {
	stubProvider
}

type rejectHTTPSubjectProvider struct {
	stubProvider
}

func (p *sessionCatalogStubProvider) CatalogForRequest(ctx context.Context, _ string) (*proto.Catalog, error) {
	cat := protoutil.Clone(p.sessionCatalog).(*proto.Catalog)
	if cat != nil {
		subject := gestalt.SubjectFromContext(ctx)
		credential := gestalt.CredentialFromContext(ctx)
		access := gestalt.AccessFromContext(ctx)
		cat.DisplayName = subject.ID + "|" + credential.Mode + "|" + access.Policy + "|" + access.Role
	}
	return cat, nil
}

func (p *panicHTTPSubjectProvider) testOp(ctx context.Context, input stubInput, req gestalt.Request) (gestalt.Response[stubOutput], error) {
	return p.stubProvider.testOp(ctx, input, req)
}

func (p *panicHTTPSubjectProvider) ResolveHTTPSubject(_ context.Context, _ gestalt.HTTPSubjectRequest) (*gestalt.Subject, error) {
	panic("boom")
}

func (p *rejectHTTPSubjectProvider) ResolveHTTPSubject(_ context.Context, _ gestalt.HTTPSubjectRequest) (*gestalt.Subject, error) {
	return nil, gestalt.Error(http.StatusForbidden, "unmapped slack subject")
}

func TestProviderServerGetMetadata(t *testing.T) {
	t.Parallel()

	t.Run("plain provider", func(t *testing.T) {
		client := newIntegrationProviderClient(t, &stubProvider{}, stubRouter)
		meta, err := client.GetMetadata(context.Background(), &emptypb.Empty{})
		if err != nil {
			t.Fatalf("GetMetadata: %v", err)
		}
		if meta.GetSupportsSessionCatalog() {
			t.Fatal("SupportsSessionCatalog = true, want false")
		}
		if meta.GetMinProtocolVersion() != proto.CurrentProtocolVersion {
			t.Fatalf("MinProtocolVersion = %d, want %d", meta.GetMinProtocolVersion(), proto.CurrentProtocolVersion)
		}
		if meta.GetMaxProtocolVersion() != proto.CurrentProtocolVersion {
			t.Fatalf("MaxProtocolVersion = %d, want %d", meta.GetMaxProtocolVersion(), proto.CurrentProtocolVersion)
		}
	})

	t.Run("session catalog provider", func(t *testing.T) {
		client := newIntegrationProviderClient(t, &sessionCatalogStubProvider{
			sessionCatalog: &proto.Catalog{
				Name: "test-provider",
				Operations: []*proto.CatalogOperation{
					{Id: "session_op", Method: http.MethodGet, AllowedRoles: []string{"viewer"}},
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
		if meta.GetMinProtocolVersion() != proto.CurrentProtocolVersion {
			t.Fatalf("MinProtocolVersion = %d, want %d", meta.GetMinProtocolVersion(), proto.CurrentProtocolVersion)
		}
		if meta.GetMaxProtocolVersion() != proto.CurrentProtocolVersion {
			t.Fatalf("MaxProtocolVersion = %d, want %d", meta.GetMaxProtocolVersion(), proto.CurrentProtocolVersion)
		}
	})
}

func TestProviderServerGetSessionCatalog(t *testing.T) {
	t.Parallel()

	t.Run("supported", func(t *testing.T) {
		prov := &sessionCatalogStubProvider{
			sessionCatalog: &proto.Catalog{
				Name: "test-provider",
				Operations: []*proto.CatalogOperation{
					{Id: "session_op", Method: http.MethodPost, AllowedRoles: []string{"viewer"}},
				},
			},
		}
		client := newIntegrationProviderClient(t, prov, sessionCatalogStubRouter)
		resp, err := client.GetSessionCatalog(context.Background(), &proto.GetSessionCatalogRequest{
			Token: "tok",
			Context: &proto.RequestContext{
				Subject: &proto.SubjectContext{
					Id:   "user:user-123",
					Kind: "user",
				},
				Credential: &proto.CredentialContext{
					Mode: "identity",
				},
				Access: &proto.AccessContext{
					Policy: "roadmap",
					Role:   "viewer",
				},
			},
		})
		if err != nil {
			t.Fatalf("GetSessionCatalog: %v", err)
		}
		if resp.GetCatalog() == nil {
			t.Fatal("expected session catalog")
		}
		if resp.GetCatalog().GetDisplayName() != "user:user-123|identity|roadmap|viewer" {
			t.Fatalf("DisplayName = %q, want %q", resp.GetCatalog().GetDisplayName(), "user:user-123|identity|roadmap|viewer")
		}
		if got := resp.GetCatalog().GetOperations()[0].GetAllowedRoles(); len(got) != 1 || got[0] != "viewer" {
			t.Fatalf("AllowedRoles = %#v, want %#v", got, []string{"viewer"})
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		client := newIntegrationProviderClient(t, &stubProvider{}, stubRouter)
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
			wantBody:   `{"operation":"test_op","subject_id":"user:user-123","subject_kind":"user","credential_mode":"identity","credential_subject_id":"identity:__identity__","access_policy":"roadmap","access_role":"admin"}`,
			request: &proto.ExecuteRequest{
				Operation: "test_op",
				Params: func() *structpb.Struct {
					params, _ := structpb.NewStruct(map[string]any{"key": "value"})
					return params
				}(),
				Token: "tok",
				Context: &proto.RequestContext{
					Subject: &proto.SubjectContext{
						Id:   "user:user-123",
						Kind: "user",
					},
					Credential: &proto.CredentialContext{
						Mode:      "identity",
						SubjectId: "identity:__identity__",
					},
					Access: &proto.AccessContext{
						Policy: "roadmap",
						Role:   "admin",
					},
				},
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
			wantBody:   `{"error":"internal error"}`,
			request: &proto.ExecuteRequest{
				Operation: "error_op",
			},
		},
		{
			name:       "panic",
			router:     panicRouter,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"internal error"}`,
			request: &proto.ExecuteRequest{
				Operation: "panic_op",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newIntegrationProviderClient(t, &stubProvider{}, tt.router)

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

	t.Run("http subject panic", func(t *testing.T) {
		panicHTTPSubjectRouter := gestalt.MustRouter(
			gestalt.Register(
				gestalt.Operation[stubInput, stubOutput]{
					ID:     "test_op",
					Method: http.MethodPost,
				},
				(*panicHTTPSubjectProvider).testOp,
			),
		)
		client := newIntegrationProviderClient(t, &panicHTTPSubjectProvider{}, panicHTTPSubjectRouter)

		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		defer func() { _ = reader.Close() }()

		oldStderr := os.Stderr
		os.Stderr = writer
		defer func() {
			os.Stderr = oldStderr
		}()

		_, err = client.ResolveHTTPSubject(context.Background(), &proto.ResolveHTTPSubjectRequest{
			Request: &proto.HTTPSubjectRequest{
				Binding: "command",
			},
		})
		os.Stderr = oldStderr
		_ = writer.Close()
		output, readErr := io.ReadAll(reader)
		if readErr != nil {
			t.Fatalf("io.ReadAll: %v", readErr)
		}
		if status.Code(err) != codes.Internal {
			t.Fatalf("ResolveHTTPSubject code = %v, want %v (err=%v)", status.Code(err), codes.Internal, err)
		}
		if !strings.Contains(string(output), `panic in Gestalt operation "ResolveHTTPSubject": boom`) {
			t.Fatalf("stderr = %q, want panic log", string(output))
		}
	})

	t.Run("http subject rejection", func(t *testing.T) {
		rejectHTTPSubjectRouter := gestalt.MustRouter(
			gestalt.Register(
				gestalt.Operation[stubInput, stubOutput]{
					ID:     "test_op",
					Method: http.MethodPost,
				},
				(*rejectHTTPSubjectProvider).testOp,
			),
		)
		client := newIntegrationProviderClient(t, &rejectHTTPSubjectProvider{}, rejectHTTPSubjectRouter)

		resp, err := client.ResolveHTTPSubject(context.Background(), &proto.ResolveHTTPSubjectRequest{
			Request: &proto.HTTPSubjectRequest{
				Binding: "command",
			},
		})
		if err != nil {
			t.Fatalf("ResolveHTTPSubject: %v", err)
		}
		if resp.GetRejectStatus() != http.StatusForbidden {
			t.Fatalf("RejectStatus = %d, want %d", resp.GetRejectStatus(), http.StatusForbidden)
		}
		if resp.GetRejectMessage() != "unmapped slack subject" {
			t.Fatalf("RejectMessage = %q, want %q", resp.GetRejectMessage(), "unmapped slack subject")
		}
	})
}

func TestProviderServerStartProvider(t *testing.T) {
	t.Parallel()

	t.Run("accepts matching protocol version", func(t *testing.T) {
		prov := &startableStubProvider{}
		client := newIntegrationProviderClient(t, prov, startableStubRouter)
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
	})

	t.Run("rejects mismatched protocol version", func(t *testing.T) {
		prov := &startableStubProvider{}
		client := newIntegrationProviderClient(t, prov, startableStubRouter)
		ctx := context.Background()

		_, err := client.StartProvider(ctx, &proto.StartProviderRequest{
			Name:            "my-instance",
			Config:          &structpb.Struct{},
			ProtocolVersion: proto.CurrentProtocolVersion + 1,
		})
		if err == nil {
			t.Fatal("StartProvider should fail for mismatched protocol version")
		}
		if code := status.Code(err); code != codes.FailedPrecondition {
			t.Fatalf("StartProvider code = %v, want %v", code, codes.FailedPrecondition)
		}
		if prov.name != "" {
			t.Fatalf("provider configured name = %q, want empty", prov.name)
		}
		if prov.config != nil {
			t.Fatalf("provider config = %#v, want nil", prov.config)
		}
	})
}
