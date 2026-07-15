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
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	gproto "google.golang.org/protobuf/proto"
)

type stubProvider struct{}

type stubInput struct{}

type stubOutput struct {
	Operation           string `json:"operation"`
	SubjectID           string `json:"subject_id"`
	SubjectKind         string `json:"subject_kind"`
	SubjectEmail        string `json:"subject_email,omitempty"`
	AgentSubjectID      string `json:"agent_subject_id,omitempty"`
	AgentSubjectEmail   string `json:"agent_subject_email,omitempty"`
	CredentialMode      string `json:"credential_mode"`
	CredentialSubjectID string `json:"credential_subject_id"`
	AccessPolicy        string `json:"access_policy"`
	AccessRole          string `json:"access_role"`
	HostBaseURL         string `json:"host_base_url"`
	ToolRefsSet         bool   `json:"tool_refs_set,omitempty"`
	ToolRefApp          string `json:"tool_ref_app,omitempty"`
	ToolRefOperation    string `json:"tool_ref_operation,omitempty"`
	IdempotencyKey      string `json:"idempotency_key"`
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

var workflowDeclarationsStubRouter = gestalt.MustRouter(
	gestalt.Register(
		gestalt.Operation[stubInput, stubOutput]{
			ID:     "test_op",
			Method: http.MethodPost,
		},
		(*workflowDeclarationsStubProvider).testOp,
	),
)

func (p *stubProvider) Configure(_ context.Context, _ string, _ map[string]any) error {
	return nil
}

func (p *stubProvider) testOp(_ context.Context, _ stubInput, req gestalt.Request) (gestalt.Response[stubOutput], error) {
	subjectKind, _, _ := gestalt.ParseSubjectID(req.Subject.ID)
	out := stubOutput{
		Operation:           "test_op",
		SubjectID:           req.Subject.ID,
		SubjectKind:         subjectKind,
		SubjectEmail:        req.Subject.Email,
		AgentSubjectID:      req.AgentSubject.ID,
		AgentSubjectEmail:   req.AgentSubject.Email,
		CredentialMode:      req.Credential.Mode,
		CredentialSubjectID: req.Credential.SubjectID,
		AccessPolicy:        req.Access.Policy,
		AccessRole:          req.Access.Role,
		HostBaseURL:         req.Host.PublicBaseURL,
		ToolRefsSet:         req.ToolRefsSet,
		IdempotencyKey:      req.IdempotencyKey,
	}
	if len(req.ToolRefs) > 0 {
		out.ToolRefApp = req.ToolRefs[0].App
		out.ToolRefOperation = req.ToolRefs[0].Operation
	}
	return gestalt.OK(out), nil
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

type panicHTTPSubjectProvider struct {
	stubProvider
}

type rejectHTTPSubjectProvider struct {
	stubProvider
}

type workflowDeclarationsStubProvider struct {
	stubProvider
}

func (p *workflowDeclarationsStubProvider) DeclaredWorkflowDefinitions() ([]gestalt.WorkflowDefinitionSpec, error) {
	return []gestalt.WorkflowDefinitionSpec{{
		ID:    "daily-summary",
		RunAs: "service_account:sa1",
	}}, nil
}

func (p *sessionCatalogStubProvider) CatalogForRequest(ctx context.Context, _ string) (*gestalt.Catalog, error) {
	cat := cloneTestCatalog(p.sessionCatalog)
	if cat != nil {
		subject := gestalt.SubjectFromContext(ctx)
		credential := gestalt.CredentialFromContext(ctx)
		access := gestalt.AccessFromContext(ctx)
		host := gestalt.HostContextFromContext(ctx)
		cat.DisplayName = subject.ID + "|" + credential.Mode + "|" + access.Policy + "|" + access.Role + "|" + host.PublicBaseURL
	}
	return cat, nil
}

func cloneTestCatalog(src *gestalt.Catalog) *gestalt.Catalog {
	if src == nil {
		return nil
	}
	out := &gestalt.Catalog{
		Name:        src.Name,
		DisplayName: src.DisplayName,
		Description: src.Description,
		IconSvg:     src.IconSvg,
		Operations:  make([]*gestalt.CatalogOperation, 0, len(src.Operations)),
	}
	for _, op := range src.Operations {
		if op == nil {
			out.Operations = append(out.Operations, nil)
			continue
		}
		out.Operations = append(out.Operations, &gestalt.CatalogOperation{
			Id:             op.Id,
			Method:         op.Method,
			Title:          op.Title,
			Description:    op.Description,
			InputSchema:    op.InputSchema,
			OutputSchema:   op.OutputSchema,
			Annotations:    cloneTestOperationAnnotations(op.Annotations),
			Parameters:     cloneTestCatalogParameters(op.Parameters),
			RequiredScopes: append([]string(nil), op.RequiredScopes...),
			Tags:           append([]string(nil), op.Tags...),
			ReadOnly:       op.ReadOnly,
			Visible:        cloneTestBool(op.Visible),
			Transport:      op.Transport,
			AllowedRoles:   append([]string(nil), op.AllowedRoles...),
		})
	}
	return out
}

func cloneTestOperationAnnotations(src *gestalt.OperationAnnotations) *gestalt.OperationAnnotations {
	if src == nil {
		return nil
	}
	return &gestalt.OperationAnnotations{
		ReadOnlyHint:    cloneTestBool(src.ReadOnlyHint),
		IdempotentHint:  cloneTestBool(src.IdempotentHint),
		DestructiveHint: cloneTestBool(src.DestructiveHint),
		OpenWorldHint:   cloneTestBool(src.OpenWorldHint),
	}
}

func cloneTestCatalogParameters(src []*gestalt.CatalogParameter) []*gestalt.CatalogParameter {
	out := make([]*gestalt.CatalogParameter, 0, len(src))
	for _, param := range src {
		if param == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &gestalt.CatalogParameter{
			Name:        param.Name,
			Type:        param.Type,
			Description: param.Description,
			Required:    param.Required,
			Default:     param.Default,
			HasDefault:  param.HasDefault,
		})
	}
	return out
}

func cloneTestBool(src *bool) *bool {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func (p *panicHTTPSubjectProvider) testOp(ctx context.Context, input stubInput, req gestalt.Request) (gestalt.Response[stubOutput], error) {
	return p.stubProvider.testOp(ctx, input, req)
}

func (p *panicHTTPSubjectProvider) ResolveHTTPSubject(_ context.Context, _ gestalt.HTTPSubjectRequest) (*gestalt.Subject, error) {
	panic("boom")
}

func (p *rejectHTTPSubjectProvider) ResolveHTTPSubject(_ context.Context, _ gestalt.HTTPSubjectRequest) (*gestalt.Subject, error) {
	return nil, gestalt.Error(http.StatusForbidden, "unmapped test subject")
}

func TestProviderServerGetMetadata(t *testing.T) {
	t.Parallel()

	t.Run("plain provider", func(t *testing.T) {
		client := newAppProviderClient(t, &stubProvider{}, stubRouter)
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
		client := newAppProviderClient(t, &sessionCatalogStubProvider{
			sessionCatalog: &gestalt.Catalog{
				Name: "test-provider",
				Operations: []*gestalt.CatalogOperation{
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

	t.Run("workflow declarations provider", func(t *testing.T) {
		client := newAppProviderClient(t, &workflowDeclarationsStubProvider{}, workflowDeclarationsStubRouter)
		meta, err := client.GetMetadata(context.Background(), &emptypb.Empty{})
		if err != nil {
			t.Fatalf("GetMetadata: %v", err)
		}
		if len(meta.GetWorkflowDefinitionSpecs()) != 1 {
			t.Fatalf("WorkflowDefinitionSpecs = %d, want 1", len(meta.GetWorkflowDefinitionSpecs()))
		}
		wire := &proto.WorkflowDefinitionSpec{}
		if err := gproto.Unmarshal(meta.GetWorkflowDefinitionSpecs()[0], wire); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if wire.GetId() != "daily-summary" || wire.GetRunAs() != "service_account:sa1" {
			t.Fatalf("wire = %#v", wire)
		}
	})

}

func TestProviderServerGetSessionCatalog(t *testing.T) {
	t.Parallel()

	t.Run("supported", func(t *testing.T) {
		prov := &sessionCatalogStubProvider{
			sessionCatalog: &gestalt.Catalog{
				Name: "test-provider",
				Operations: []*gestalt.CatalogOperation{
					{
						Id:           "session_op",
						Method:       http.MethodPost,
						InputSchema:  `{"type":"object","properties":{"count":{"type":"integer","default":5}}}`,
						OutputSchema: `{"type":"object","properties":{"ok":{"type":"boolean"}}}`,
						Parameters: []*gestalt.CatalogParameter{
							{Name: "count", Type: "integer", Default: float64(5), HasDefault: true},
							{Name: "optional", Type: "object", HasDefault: true},
						},
						AllowedRoles: []string{"viewer"},
					},
				},
			},
		}
		client := newAppProviderClient(t, prov, sessionCatalogStubRouter)
		resp, err := client.GetSessionCatalog(context.Background(), &proto.GetSessionCatalogRequest{
			Token: "tok",
			Context: &proto.RequestContext{
				Subject: &proto.SubjectContext{
					Id: "user:user-123",
				},
				Credential: &proto.CredentialContext{
					Mode: "subject",
				},
				Access: &proto.AccessContext{
					Policy: "roadmap",
					Role:   "viewer",
				},
				Host: &proto.HostContext{
					PublicBaseUrl: "https://gestalt.example.test",
				},
			},
		})
		if err != nil {
			t.Fatalf("GetSessionCatalog: %v", err)
		}
		if resp.GetCatalog() == nil {
			t.Fatal("expected session catalog")
		}
		if resp.GetCatalog().GetDisplayName() != "user:user-123|subject|roadmap|viewer|https://gestalt.example.test" {
			t.Fatalf("DisplayName = %q, want %q", resp.GetCatalog().GetDisplayName(), "user:user-123|subject|roadmap|viewer|https://gestalt.example.test")
		}
		if got := resp.GetCatalog().GetOperations()[0].GetAllowedRoles(); len(got) != 1 || got[0] != "viewer" {
			t.Fatalf("AllowedRoles = %#v, want %#v", got, []string{"viewer"})
		}
		op := resp.GetCatalog().GetOperations()[0]
		if op.GetInputSchema() == "" || op.GetOutputSchema() == "" {
			t.Fatalf("schemas were not preserved: input=%q output=%q", op.GetInputSchema(), op.GetOutputSchema())
		}
		if got := op.GetParameters()[0].GetDefault().GetNumberValue(); got != 5 {
			t.Fatalf("parameter default = %v, want 5", got)
		}
		if got := op.GetParameters()[1].GetDefault().GetNullValue(); got != structpb.NullValue_NULL_VALUE {
			t.Fatalf("null parameter default = %v, want NULL_VALUE", got)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		client := newAppProviderClient(t, &stubProvider{}, stubRouter)
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
			wantBody:   `{"operation":"test_op","subject_id":"user:user-123","subject_kind":"user","subject_email":"ada@example.com","agent_subject_id":"user:user-456","agent_subject_email":"grace@example.com","credential_mode":"subject","credential_subject_id":"user:user-123","access_policy":"roadmap","access_role":"admin","host_base_url":"https://gestalt.example.test","tool_refs_set":true,"tool_ref_app":"target","tool_ref_operation":"reviews.get","idempotency_key":"tool-call-123"}`,
			request: &proto.ExecuteRequest{
				Operation: "test_op",
				Params: func() *structpb.Struct {
					params, _ := structpb.NewStruct(map[string]any{"key": "value"})
					return params
				}(),
				Token: "tok",
				Context: &proto.RequestContext{
					Subject: &proto.SubjectContext{
						Id:    "user:user-123",
						Email: "ada@example.com",
					},
					AgentSubject: &proto.SubjectContext{
						Id:    "user:user-456",
						Email: "grace@example.com",
					},
					Credential: &proto.CredentialContext{
						Mode:      "subject",
						SubjectId: "user:user-123",
					},
					Access: &proto.AccessContext{
						Policy: "roadmap",
						Role:   "admin",
					},
					Host: &proto.HostContext{
						PublicBaseUrl: "https://gestalt.example.test",
					},
					ToolRefs: []*proto.AgentToolRef{{
						App:       "target",
						Operation: "reviews.get",
					}},
					ToolRefsSet: true,
				},
				IdempotencyKey: " tool-call-123 ",
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
			client := newAppProviderClient(t, &stubProvider{}, tt.router)

			resp, err := client.Execute(context.Background(), tt.request)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if resp.GetStatus() != tt.wantStatus {
				t.Fatalf("Status = %d, want %d", resp.GetStatus(), tt.wantStatus)
			}
			body := string(resp.GetBody())
			if tt.wantBody != "" && body != tt.wantBody {
				t.Fatalf("Body = %q, want %q", resp.GetBody(), tt.wantBody)
			}
			for _, want := range tt.wantBodyContain {
				if !strings.Contains(body, want) {
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
		client := newAppProviderClient(t, &panicHTTPSubjectProvider{}, panicHTTPSubjectRouter)

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
		client := newAppProviderClient(t, &rejectHTTPSubjectProvider{}, rejectHTTPSubjectRouter)

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
		if resp.GetRejectMessage() != "unmapped test subject" {
			t.Fatalf("RejectMessage = %q, want %q", resp.GetRejectMessage(), "unmapped test subject")
		}
	})
}

func TestProviderServerStartProvider(t *testing.T) {
	t.Parallel()

	t.Run("accepts matching protocol version", func(t *testing.T) {
		prov := &startableStubProvider{}
		client := newAppProviderClient(t, prov, startableStubRouter)
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
		client := newAppProviderClient(t, prov, startableStubRouter)
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
