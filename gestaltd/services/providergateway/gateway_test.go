package providergateway

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

func boolPtr(value bool) *bool {
	return &value
}

type stubAuthorizationProvider struct {
	called        bool
	allowedResult *bool
	ctx           context.Context
	request       *proto.CheckAccessRequest
}

func (p *stubAuthorizationProvider) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.called = true
	p.ctx = ctx
	p.request = req
	allowed := true
	if p.allowedResult != nil {
		allowed = *p.allowedResult
	}
	return &proto.CheckAccessResponse{Allowed: allowed}, nil
}

func (p *stubAuthorizationProvider) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{}, nil
}

func (p *stubAuthorizationProvider) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func (p *stubAuthorizationProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (p *stubAuthorizationProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *stubAuthorizationProvider) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}

func (p *stubAuthorizationProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (p *stubAuthorizationProvider) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}

func (p *stubAuthorizationProvider) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}

func (p *stubAuthorizationProvider) Ping(context.Context) error { return nil }

func (p *stubAuthorizationProvider) Close() error { return nil }

func TestPreparePublicRequest(t *testing.T) {
	t.Parallel()

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}

	activeAlice := &core.IntrospectResponse{Active: true, Subject: "user:alice"}
	inactive := &core.IntrospectResponse{Active: false}
	denied := false

	tests := []struct {
		name         string
		fullMethod   string
		withOrigin   bool
		metadata     []string
		introspect   *core.IntrospectResponse
		authAllow    *bool
		req          gproto.Message
		wantCode     codes.Code
		wantSubject  string
		setup        func(*ProviderGatewayTransport)
		checkAdapted func(t *testing.T, adapted gproto.Message)
		checkAuth    func(t *testing.T, auth *stubAuthorizationProvider)
	}{
		{
			name:       "app invoke fills context",
			fullMethod: proto.App_Invoke_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			authAllow:  &denied,
			req:        &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"},
			checkAdapted: func(t *testing.T, adapted gproto.Message) {
				t.Helper()
				out, ok := adapted.(*proto.AppInvokeRequest)
				if !ok {
					t.Fatalf("adapted type = %T, want *proto.AppInvokeRequest", adapted)
				}
				if out.GetContext() == nil || out.GetContext().GetSubject().GetId() != "user:alice" {
					t.Fatalf("context subject = %#v, want user:alice", out.GetContext())
				}
				if out.GetRunAs() != nil {
					t.Fatal("run_as should remain unset")
				}
			},
		},
		{
			name:       "app invoke rejects run_as",
			fullMethod: proto.App_Invoke_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			req: &proto.AppInvokeRequest{
				App:       "roadmap",
				Operation: "sync",
				RunAs:     &proto.SubjectContext{Id: "user:bob"},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name:       "app invoke rejects client context",
			fullMethod: proto.App_Invoke_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			req: &proto.AppInvokeRequest{
				App:       "roadmap",
				Operation: "sync",
				Context:   &proto.RequestContext{Subject: &proto.SubjectContext{Id: "user:bob"}},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name:       "agent create session fills context subject",
			fullMethod: proto.Agent_CreateSession_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			req:        &proto.CreateAgentProviderSessionRequest{Model: "gpt-test"},
			checkAdapted: func(t *testing.T, adapted gproto.Message) {
				t.Helper()
				out, ok := adapted.(*proto.CreateAgentProviderSessionRequest)
				if !ok {
					t.Fatalf("adapted type = %T, want *proto.CreateAgentProviderSessionRequest", adapted)
				}
				if out.GetContext().GetSubject().GetId() != "user:alice" {
					t.Fatalf("context.subject.id = %q, want %q", out.GetContext().GetSubject().GetId(), "user:alice")
				}
			},
		},
		{
			name:       "workflow deliver event is internal only",
			fullMethod: proto.Workflow_DeliverEvent_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			req:        &proto.DeliverWorkflowProviderEventRequest{Event: &proto.WorkflowEvent{}},
			wantCode:   codes.NotFound,
		},
		{
			name:       "app invoke rejects empty app",
			fullMethod: proto.App_Invoke_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			req:        &proto.AppInvokeRequest{Operation: "sync"},
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "requires authorization provider",
			fullMethod: proto.Workflow_DeliverEvent_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			req:        &proto.DeliverWorkflowProviderEventRequest{Event: &proto.WorkflowEvent{}},
			wantCode:   codes.NotFound,
			setup: func(transport *ProviderGatewayTransport) {
				transport.SetAuthorizationProvider(nil)
			},
		},
		{
			name:       "workflow authorization denied",
			fullMethod: proto.Workflow_DeliverEvent_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			authAllow:  &denied,
			req:        &proto.DeliverWorkflowProviderEventRequest{Event: &proto.WorkflowEvent{}},
			wantCode:   codes.NotFound,
		},
		{
			name:       "identity authorize skips bearer and provider authorization",
			fullMethod: proto.Identity_Authorize_FullMethodName,
			withOrigin: true,
			metadata:   []string{},
			authAllow:  &denied,
			req: &proto.AuthorizeRequest{
				ResponseType: "code",
				ClientId:     "gestalt-cli",
				RedirectUri:  "http://localhost:8080/api/v1/auth/login/callback",
				State:        "login-state",
			},
		},
		{
			name:       "identity failure",
			fullMethod: proto.App_Invoke_FullMethodName,
			withOrigin: true,
			introspect: inactive,
			req:        &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"},
			wantCode:   codes.Unauthenticated,
		},
		{
			name:       "rejects caller bearer metadata",
			fullMethod: proto.App_Invoke_FullMethodName,
			withOrigin: true,
			metadata:   []string{"x-gestalt-caller-bearer-token", "test-token"},
			introspect: activeAlice,
			req:        &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"},
			wantCode:   codes.Unauthenticated,
		},
		{
			name:       "requires public origin",
			fullMethod: proto.App_Invoke_FullMethodName,
			introspect: activeAlice,
			req:        &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"},
			wantCode:   codes.Internal,
		},
		{
			name:       "external credentials list uses persisted user subject",
			fullMethod: proto.ExternalCredentials_ListCredentials_FullMethodName,
			withOrigin: true,
			introspect: &core.IntrospectResponse{Active: true, Subject: "user:alice@example.com"},
			req:        &proto.ListExternalCredentialsRequest{},
			setup: func(transport *ProviderGatewayTransport) {
				transport.SetUserStore(stubGatewayUserStore{ids: map[string]string{"alice@example.com": "db-alice"}})
			},
			wantSubject: "user:db-alice",
			checkAdapted: func(t *testing.T, adapted gproto.Message) {
				t.Helper()
				out, ok := adapted.(*proto.ListExternalCredentialsRequest)
				if !ok {
					t.Fatalf("adapted type = %T, want *proto.ListExternalCredentialsRequest", adapted)
				}
				if got := out.GetSubject(); got != "user:db-alice" {
					t.Fatalf("subject = %q, want %q", got, "user:db-alice")
				}
			},
		},
		{
			name:       "external credentials resolve leaves actor subject empty",
			fullMethod: proto.ExternalCredentials_ResolveCredential_FullMethodName,
			withOrigin: true,
			introspect: &core.IntrospectResponse{Active: true, Subject: "user:alice@example.com"},
			req:        &proto.ResolveExternalCredentialRequest{Provider: "github", Connection: "default"},
			setup: func(transport *ProviderGatewayTransport) {
				transport.SetUserStore(stubGatewayUserStore{ids: map[string]string{"alice@example.com": "db-alice"}})
			},
			wantSubject: "user:db-alice",
			checkAdapted: func(t *testing.T, adapted gproto.Message) {
				t.Helper()
				out, ok := adapted.(*proto.ResolveExternalCredentialRequest)
				if !ok {
					t.Fatalf("adapted type = %T, want *proto.ResolveExternalCredentialRequest", adapted)
				}
				if got := out.GetCredentialSubjectId(); got != "user:db-alice" {
					t.Fatalf("credential_subject_id = %q, want %q", got, "user:db-alice")
				}
				if got := out.GetActorSubjectId(); got != "" {
					t.Fatalf("actor_subject_id = %q, want empty", got)
				}
			},
		},
		{
			name:       "app invoke canonicalizes email subject to persisted user id",
			fullMethod: proto.App_Invoke_FullMethodName,
			withOrigin: true,
			introspect: &core.IntrospectResponse{Active: true, Subject: "user:alice@example.com"},
			req:        &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"},
			setup: func(transport *ProviderGatewayTransport) {
				transport.SetUserStore(stubGatewayUserStore{ids: map[string]string{"alice@example.com": "db-alice"}})
			},
			wantSubject: "user:db-alice",
			checkAdapted: func(t *testing.T, adapted gproto.Message) {
				t.Helper()
				out, ok := adapted.(*proto.AppInvokeRequest)
				if !ok {
					t.Fatalf("adapted type = %T, want *proto.AppInvokeRequest", adapted)
				}
				if got := out.GetContext().GetSubject().GetId(); got != "user:db-alice" {
					t.Fatalf("context subject = %q, want canonical user:db-alice", got)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			authorization := &stubAuthorizationProvider{allowedResult: tc.authAllow}
			identity := &coretesting.StubAuthProvider{
				IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
					return tc.introspect, nil
				},
			}
			transport := NewProviderGatewayTransport()
			transport.SetPublicMethods(registry)
			transport.SetIdentityProvider(identity)
			transport.SetAuthorizationProvider(authorization)
			transport.SetPublicBaseURL("https://gestalt.example")
			if tc.setup != nil {
				tc.setup(transport)
			}

			ctx := context.Background()
			if tc.withOrigin {
				ctx = publicrpc.WithPublicOrigin(ctx, tc.fullMethod)
			}
			metadataPairs := tc.metadata
			if metadataPairs == nil {
				metadataPairs = []string{"authorization", "Bearer test-token"}
			}
			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(metadataPairs...))

			p, adapted, err := transport.PreparePublicRequest(ctx, tc.fullMethod, tc.req)
			if tc.wantCode != codes.OK {
				assertGRPCCode(t, err, tc.wantCode)
				return
			}
			if err != nil {
				t.Fatalf("PreparePublicRequest: %v", err)
			}
			if p != nil {
				want := wantSubjectID(tc.wantSubject, "user:alice")
				if p.SubjectID != want {
					t.Fatalf("SubjectID = %q, want %q", p.SubjectID, want)
				}
			}
			if tc.checkAdapted != nil {
				tc.checkAdapted(t, adapted)
			}
			if tc.checkAuth != nil {
				if !authorization.called {
					t.Fatal("authorization provider was not called")
				}
				tc.checkAuth(t, authorization)
			}
		})
	}
}

func TestPublicResourceIDRequiresWorkflowProvider(t *testing.T) {
	t.Parallel()

	transport := NewProviderGatewayTransport()
	_, err := transport.publicResourceID(
		&proto.GetWorkflowProviderDefinitionRequest{DefinitionId: "definition-1"},
		proto.Workflow_GetDefinition_FullMethodName,
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status.Code(err) = %v, want InvalidArgument (%v)", status.Code(err), err)
	}

	resourceID, err := transport.publicResourceID(
		&proto.GetWorkflowProviderDefinitionRequest{Provider: "temporal", DefinitionId: "definition-1"},
		proto.Workflow_GetDefinition_FullMethodName,
	)
	if err != nil || resourceID != "temporal" {
		t.Fatalf("resource ID = %q, err = %v, want temporal", resourceID, err)
	}
}

func assertGRPCCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %v", want)
	}
	if status.Code(err) != want {
		t.Fatalf("status.Code(err) = %v, want %v (%v)", status.Code(err), want, err)
	}
}

func wantSubjectID(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}

type stubGatewayUserStore struct {
	ids map[string]string
}

func (s stubGatewayUserStore) FindOrCreateUser(_ context.Context, email string) (*core.User, error) {
	id, ok := s.ids[email]
	if !ok {
		return nil, errors.New("user not found")
	}
	return &core.User{ID: id, Email: email}, nil
}

// --- routing gateway tests ---

type stubConn struct {
	invoke func(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error
}

func (c *stubConn) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	if c.invoke != nil {
		return c.invoke(ctx, method, args, reply, opts...)
	}
	return nil
}

func (c *stubConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.Unimplemented, "stubConn does not support streaming")
}

type stubStreamConn struct{}

func (c *stubStreamConn) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	return nil
}

func (c *stubStreamConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return &stubClientStream{}, nil
}

type stubClientStream struct{}

func (s *stubClientStream) Header() (metadata.MD, error) { return nil, nil }
func (s *stubClientStream) Trailer() metadata.MD         { return nil }
func (s *stubClientStream) CloseSend() error             { return nil }
func (s *stubClientStream) Context() context.Context     { return context.Background() }
func (s *stubClientStream) SendMsg(m any) error          { return nil }
func (s *stubClientStream) RecvMsg(m any) error          { return io.EOF }

func TestRoutingInvokeForwardsToEndpoint(t *testing.T) {
	t.Parallel()

	called := false
	conn := &stubConn{invoke: func(_ context.Context, method string, args any, reply any, _ ...grpc.CallOption) error {
		called = true
		if method != "/test.Service/Echo" {
			t.Fatalf("method = %q, want /test.Service/Echo", method)
		}
		return nil
	}}
	transport := NewProviderGatewayTransport()
	target := ProviderTarget{Kind: "test", Name: "stub"}
	if err := transport.RegisterDirect(target, DirectEndpoint{Conn: conn}); err != nil {
		t.Fatalf("RegisterDirect: %v", err)
	}
	if err := transport.Conn(target).Invoke(context.Background(), "/test.Service/Echo", nil, nil); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !called {
		t.Fatal("endpoint connection was not invoked")
	}
}

func TestRoutingInvokeMissingTargetReturnsNotFound(t *testing.T) {
	t.Parallel()

	transport := NewProviderGatewayTransport()
	target := ProviderTarget{Kind: "test", Name: "missing"}
	err := transport.Conn(target).Invoke(context.Background(), "/test.Service/Echo", nil, nil)
	if status.Code(err) != codes.NotFound {
		t.Fatalf("status.Code(err) = %v, want NotFound (%v)", status.Code(err), err)
	}
}

func TestRoutingGatewayAuthorization(t *testing.T) {
	t.Parallel()

	parityTarget := ProviderTarget{Kind: "test", Name: "parity"}
	authzTarget := ProviderTarget{Kind: ProviderKindAuthorization, Name: "authz-primary"}

	tests := []struct {
		name        string
		target      ProviderTarget
		principal   *principal.Principal
		allowed     *bool
		wantCode    codes.Code
		wantCalled  bool
		wantSuccess bool
	}{
		{
			name:        "authorization kind exempt from meta-check",
			target:      authzTarget,
			principal:   &principal.Principal{SubjectID: "user:alice"},
			allowed:     boolPtr(false),
			wantCode:    codes.OK,
			wantCalled:  false,
			wantSuccess: true,
		},
		{
			name:        "non-authorization kind denied",
			target:      parityTarget,
			principal:   &principal.Principal{SubjectID: "user:alice"},
			allowed:     boolPtr(false),
			wantCode:    codes.PermissionDenied,
			wantCalled:  true,
			wantSuccess: false,
		},
		{
			name:        "non-authorization kind allowed",
			target:      parityTarget,
			principal:   &principal.Principal{SubjectID: "user:alice"},
			allowed:     boolPtr(true),
			wantCode:    codes.OK,
			wantCalled:  true,
			wantSuccess: true,
		},
		{
			name:       "no principal unauthenticated",
			target:     parityTarget,
			principal:  nil,
			allowed:    boolPtr(true),
			wantCode:   codes.Unauthenticated,
			wantCalled: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			transport := NewProviderGatewayTransport()
			connCalled := false
			conn := &stubConn{invoke: func(_ context.Context, _ string, _ any, reply any, _ ...grpc.CallOption) error {
				connCalled = true
				return nil
			}}
			for _, target := range []ProviderTarget{parityTarget, authzTarget} {
				if err := transport.RegisterDirect(target, DirectEndpoint{Conn: conn}); err != nil {
					t.Fatalf("RegisterDirect(%s): %v", target.Name, err)
				}
			}
			authorization := &stubAuthorizationProvider{allowedResult: tc.allowed}
			transport.SetAuthorizationProvider(authorization)

			ctx := context.Background()
			if tc.principal != nil {
				ctx = principal.WithPrincipal(ctx, tc.principal)
			}
			err := transport.Conn(tc.target).Invoke(ctx, "/test.Service/Echo", nil, nil)

			if tc.wantCalled && !authorization.called {
				t.Fatal("authorization provider was not called")
			}
			if !tc.wantCalled && authorization.called {
				t.Fatal("authorization provider was unexpectedly called")
			}
			if status.Code(err) != tc.wantCode {
				t.Fatalf("status.Code(err) = %v, want %v (%v)", status.Code(err), tc.wantCode, err)
			}
			if tc.wantSuccess && !connCalled {
				t.Fatal("endpoint connection was not invoked")
			}
		})
	}
}

func TestRoutingGatewayRecordsMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)

	transport := NewProviderGatewayTransport()
	target := ProviderTarget{Kind: "test", Name: "parity"}
	if err := transport.RegisterDirect(target, DirectEndpoint{Conn: &stubConn{}}); err != nil {
		t.Fatalf("RegisterDirect: %v", err)
	}
	authorization := &stubAuthorizationProvider{allowedResult: boolPtr(false)}
	transport.SetAuthorizationProvider(authorization)
	authCtx := principal.WithPrincipal(ctx, &principal.Principal{SubjectID: "user:alice"})

	err := transport.Conn(target).Invoke(authCtx, "/test.Service/Echo", nil, nil)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status.Code(err) = %v, want PermissionDenied", status.Code(err))
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.count", 1, map[string]string{
		"gd.provider_id":           "parity",
		"gd.provider_kind":         "test",
		"gd.operation":             "Echo",
		"gd.transport":             "direct",
		"gd.entry":                 "internal",
		"gd.caller_token_provided": "true",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.error_count", 1, map[string]string{
		"gd.provider_id":           "parity",
		"gd.provider_kind":         "test",
		"gd.operation":             "Echo",
		"gd.transport":             "direct",
		"gd.entry":                 "internal",
		"gd.caller_token_provided": "true",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.authorization.count", 1, map[string]string{
		"gd.allowed":               "false",
		"gd.caller_token_provided": "true",
		"gd.subject":               "subject/user:alice",
		"gd.resource":              "test/parity",
		"gd.action":                "parity",
		"gd.entry":                 "internal",
	})
}

func TestRoutingNewStreamForwardsToEndpoint(t *testing.T) {
	t.Parallel()

	transport := NewProviderGatewayTransport()
	target := ProviderTarget{Kind: "test", Name: "stub"}
	if err := transport.RegisterDirect(target, DirectEndpoint{Conn: &stubStreamConn{}}); err != nil {
		t.Fatalf("RegisterDirect: %v", err)
	}
	stream, err := transport.Conn(target).NewStream(
		context.Background(),
		&grpc.StreamDesc{ServerStreams: true},
		"/test.Service/Stream",
	)
	if err != nil {
		t.Fatalf("NewStream err = %v, want nil", err)
	}
	if stream == nil {
		t.Fatalf("NewStream stream = nil, want non-nil")
	}
}
