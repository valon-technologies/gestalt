package gestalt_test

import (
	"context"
	"errors"
	"net/http"
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

type webhookOutput struct {
	Name            string              `json:"name"`
	Path            string              `json:"path"`
	Method          string              `json:"method"`
	ContentType     string              `json:"content_type"`
	RawBody         string              `json:"raw_body"`
	VerifiedScheme  string              `json:"verified_scheme"`
	VerifiedSubject string              `json:"verified_subject"`
	DeliveryID      string              `json:"delivery_id"`
	Headers         map[string][]string `json:"headers"`
	Claims          map[string]string   `json:"claims"`
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

var webhookStubRouter = gestalt.MustRouter(
	gestalt.Register(
		gestalt.Operation[stubInput, webhookOutput]{
			ID:     "webhook_op",
			Method: http.MethodPost,
		},
		(*stubProvider).webhookOp,
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

func (p *stubProvider) webhookOp(_ context.Context, _ stubInput, req gestalt.Request) (gestalt.Response[webhookOutput], error) {
	if req.Webhook == nil {
		return gestalt.OK(webhookOutput{}), nil
	}
	return gestalt.OK(webhookOutput{
		Name:            req.Webhook.Name,
		Path:            req.Webhook.Path,
		Method:          req.Webhook.Method,
		ContentType:     req.Webhook.ContentType,
		RawBody:         string(req.Webhook.RawBody),
		VerifiedScheme:  req.Webhook.VerifiedScheme,
		VerifiedSubject: req.Webhook.VerifiedSubject,
		DeliveryID:      req.Webhook.DeliveryID,
		Headers:         req.Webhook.Headers,
		Claims:          req.Webhook.Claims,
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
}

func TestProviderServerExecute_PreservesWebhookContext(t *testing.T) {
	t.Parallel()

	client := newIntegrationProviderClient(t, &stubProvider{}, webhookStubRouter)
	claims, _ := structpb.NewStruct(map[string]any{"scheme": "slackSignature"})
	resp, err := client.Execute(context.Background(), &proto.ExecuteRequest{
		Operation: "webhook_op",
		Context: &proto.RequestContext{
			Webhook: &proto.WebhookContext{
				Webhook:         "slackCommand",
				Path:            "/webhooks/slack-agent/command",
				Method:          http.MethodPost,
				ContentType:     "application/x-www-form-urlencoded",
				RawBody:         []byte("text=hello"),
				Headers:         []*proto.Header{{Name: "X-Slack-Signature", Values: []string{"v0=abc"}}},
				VerifiedScheme:  "slackSignature",
				VerifiedSubject: "slack-agent/slackCommand#slackSignature",
				DeliveryId:      "delivery-123",
				Claims:          claims,
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.GetStatus() != http.StatusOK {
		t.Fatalf("Status = %d, want %d", resp.GetStatus(), http.StatusOK)
	}
	want := `{"name":"slackCommand","path":"/webhooks/slack-agent/command","method":"POST","content_type":"application/x-www-form-urlencoded","raw_body":"text=hello","verified_scheme":"slackSignature","verified_subject":"slack-agent/slackCommand#slackSignature","delivery_id":"delivery-123","headers":{"X-Slack-Signature":["v0=abc"]},"claims":{"scheme":"slackSignature"}}`
	if resp.GetBody() != want {
		t.Fatalf("Body = %q, want %q", resp.GetBody(), want)
	}
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
