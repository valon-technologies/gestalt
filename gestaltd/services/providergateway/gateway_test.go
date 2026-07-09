package providergateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

func TestAuthorizeAllowsRequests(t *testing.T) {
	t.Parallel()

	transport := NewProviderGatewayTransport()
	allowed, err := transport.Authorize(context.Background(), AuthorizationParams{
		ProviderID:  "authz-primary",
		Operation:   "CheckAccess",
		CallerToken: "caller-token",
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !allowed {
		t.Fatal("Authorize allowed = false, want true")
	}
}

func TestAuthorizeUsesAuthorizationProvider(t *testing.T) {
	t.Parallel()

	privateKeyPEM, publicKeyPEM := testGatewayAuthorizeCallerTokenKeyPair(t)
	issuer, err := NewCallerTokenIssuer(privateKeyPEM)
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}
	claims, err := GenerateCallerTokenClaims("user:alice", time.Now())
	if err != nil {
		t.Fatalf("GenerateCallerTokenClaims: %v", err)
	}
	callerToken, err := issuer.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	authorization := &stubAuthorizationProvider{}
	transport := NewProviderGatewayTransport()
	transport.SetAuthorizationProvider(authorization)
	transport.SetCallerTokenPublicKey(publicKeyPEM)

	allowed, err := transport.Authorize(context.Background(), AuthorizationParams{
		ProviderID:  "authz-primary",
		Operation:   "CheckAccess",
		CallerToken: callerToken,
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !allowed {
		t.Fatal("Authorize allowed = false, want true")
	}
	if !authorization.called {
		t.Fatal("authorization provider was not called")
	}
	if got := authorization.request.GetSubject().GetType(); got != "subject" {
		t.Fatalf("Subject.Type = %q, want %q", got, "subject")
	}
	if got := authorization.request.GetSubject().GetId(); got != "user:alice" {
		t.Fatalf("Subject.Id = %q, want %q", got, "user:alice")
	}
	if got := authorization.request.GetAction().GetName(); got != "CheckAccess" {
		t.Fatalf("Action.Name = %q, want %q", got, "CheckAccess")
	}
	if got := authorization.request.GetResource().GetType(); got != "provider" {
		t.Fatalf("Resource.Type = %q, want %q", got, "provider")
	}
	if got := authorization.request.GetResource().GetId(); got != "authz-primary" {
		t.Fatalf("Resource.Id = %q, want %q", got, "authz-primary")
	}
}

func TestAuthorizeShadowModeAllowsDeniedRequests(t *testing.T) {
	t.Parallel()

	privateKeyPEM, publicKeyPEM := testGatewayAuthorizeCallerTokenKeyPair(t)
	issuer, err := NewCallerTokenIssuer(privateKeyPEM)
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}
	claims, err := GenerateCallerTokenClaims("user:alice", time.Now())
	if err != nil {
		t.Fatalf("GenerateCallerTokenClaims: %v", err)
	}
	callerToken, err := issuer.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	authorization := &stubAuthorizationProvider{allowedResult: boolPtr(false)}
	transport := NewProviderGatewayTransport()
	transport.SetAuthorizationProvider(authorization)
	transport.SetCallerTokenPublicKey(publicKeyPEM)

	allowed, err := transport.Authorize(context.Background(), AuthorizationParams{
		ProviderID:  "authz-primary",
		Operation:   "CheckAccess",
		CallerToken: callerToken,
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !allowed {
		t.Fatal("Authorize allowed = false, want true in shadow mode")
	}
	if !authorization.called {
		t.Fatal("authorization provider was not called")
	}
}

func TestAuthorizeRecordsAuthorizationMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)

	privateKeyPEM, publicKeyPEM := testGatewayAuthorizeCallerTokenKeyPair(t)
	issuer, err := NewCallerTokenIssuer(privateKeyPEM)
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}
	claims, err := GenerateCallerTokenClaims("user:alice", time.Now())
	if err != nil {
		t.Fatalf("GenerateCallerTokenClaims: %v", err)
	}
	callerToken, err := issuer.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	authorization := &stubAuthorizationProvider{allowedResult: boolPtr(false)}
	transport := NewProviderGatewayTransport()
	transport.SetAuthorizationProvider(authorization)
	transport.SetCallerTokenPublicKey(publicKeyPEM)

	if _, err := transport.Authorize(ctx, AuthorizationParams{
		ProviderID:  "authz-primary",
		Operation:   "CheckAccess",
		CallerToken: callerToken,
	}); err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.authorization.count", 1, map[string]string{
		"gd.entry":                 "internal",
		"gd.caller_token_provided": "true",
		"gd.allowed":               "false",
		"gd.subject":               "subject/user:alice",
		"gd.resource":              "provider/authz-primary",
		"gd.action":                "CheckAccess",
	})
}

func TestProviderGatewayTransportAuthorizesThenInvokesNext(t *testing.T) {
	t.Parallel()

	transport := NewProviderGatewayTransport()
	req := ProviderGatewayRequest{
		ProviderID:   "authz-primary",
		ProviderKind: ProviderKindAuthorization,
		Operation:    "SetAuthorizationState",
	}
	nextCalled := false

	_, err := transport.Invoke(context.Background(), req, func(_ context.Context, got ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		nextCalled = true
		if got.ProviderID != req.ProviderID {
			t.Fatalf("ProviderID = %q, want %q", got.ProviderID, req.ProviderID)
		}
		return ProviderGatewayResponse{}, nil
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !nextCalled {
		t.Fatal("next was not called")
	}
}

func TestProviderGatewayTransportStoresAuthorizationProvider(t *testing.T) {
	t.Parallel()

	authorization := &stubAuthorizationProvider{}
	transport := NewProviderGatewayTransport()

	transport.SetAuthorizationProvider(authorization)

	if transport.authorization != authorization {
		t.Fatal("authorization provider was not stored")
	}
}

func TestDirectTransportInvokesNext(t *testing.T) {
	t.Parallel()

	called := false
	req := ProviderGatewayRequest{ProviderID: "authz", Operation: "CheckAccess"}
	_, err := (DirectTransport{}).Invoke(context.Background(), req, func(_ context.Context, got ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		called = true
		if got.ProviderID != req.ProviderID {
			t.Fatalf("ProviderID = %q, want %q", got.ProviderID, req.ProviderID)
		}
		return ProviderGatewayResponse{}, nil
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !called {
		t.Fatal("next was not called")
	}
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

func boolPtr(value bool) *bool {
	return &value
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

func TestDirectTransportInvokeRecordsMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	req := ProviderGatewayRequest{
		ProviderID:   "authz",
		ProviderKind: ProviderKindAuthorization,
		ServiceName:  "gestalt.v1.Authorization",
		Operation:    "CheckAccess",
	}

	_, err := (DirectTransport{}).Invoke(ctx, req, func(ctx context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		return ProviderGatewayResponse{Payload: []byte("ok")}, nil
	})
	if err != nil {
		t.Fatalf("Invoke error = %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := providerGatewayMetricAttrs(req, TransportPathDirect)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.provider_gateway.operation.duration", attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.provider_gateway.operation.error_count", attrs)
}

func TestDirectTransportInvokeRecordsErrorMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	req := ProviderGatewayRequest{
		ProviderID:   "authz",
		ProviderKind: ProviderKindAuthorization,
		ServiceName:  "gestalt.v1.Authorization",
		Operation:    "CheckAccess",
	}
	wantErr := errors.New("provider failed")

	_, err := (DirectTransport{}).Invoke(ctx, req, func(ctx context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		return ProviderGatewayResponse{}, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke error = %v, want %v", err, wantErr)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := providerGatewayMetricAttrs(req, TransportPathDirect)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.provider_gateway.operation.duration", attrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.error_count", 1, attrs)
}

func TestProviderGatewayTransportInvokeRecordsMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	transport := NewProviderGatewayTransport()
	req := ProviderGatewayRequest{
		ProviderID:   "authz",
		ProviderKind: ProviderKindAuthorization,
		ServiceName:  "gestalt.v1.Authorization",
		Operation:    "CheckAccess",
	}

	_, err := transport.Invoke(ctx, req, func(ctx context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		return ProviderGatewayResponse{Payload: []byte("ok")}, nil
	})
	if err != nil {
		t.Fatalf("Invoke error = %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := providerGatewayMetricAttrs(req, TransportPathProviderGateway)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.provider_gateway.operation.duration", attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.provider_gateway.operation.error_count", attrs)
}

func TestProviderGatewayTransportInvokeRecordsErrorMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	transport := NewProviderGatewayTransport()
	req := ProviderGatewayRequest{
		ProviderID:   "authz",
		ProviderKind: ProviderKindAuthorization,
		ServiceName:  "gestalt.v1.Authorization",
		Operation:    "CheckAccess",
	}
	wantErr := errors.New("provider failed")

	_, err := transport.Invoke(ctx, req, func(ctx context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		return ProviderGatewayResponse{}, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke error = %v, want %v", err, wantErr)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := providerGatewayMetricAttrs(req, TransportPathProviderGateway)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.provider_gateway.operation.duration", attrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.error_count", 1, attrs)
}

func providerGatewayMetricAttrs(req ProviderGatewayRequest, transportPath TransportPath) map[string]string {
	return map[string]string{
		"gd.provider_id":           req.ProviderID,
		"gd.provider_kind":         string(req.ProviderKind),
		"gd.service":               req.ServiceName,
		"gd.operation":             req.Operation,
		"gd.transport":             string(transportPath),
		"gd.entry":                 "internal",
		"gd.caller_token_provided": "false",
	}
}

func testGatewayAuthorizeCallerTokenKeyPair(t testing.TB) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyBytes})
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyBytes})
	return string(privateKeyPEM), string(publicKeyPEM)
}

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
			checkAuth: func(t *testing.T, auth *stubAuthorizationProvider) {
				t.Helper()
				if got := auth.request.GetResource().GetId(); got != "roadmap" {
					t.Fatalf("Resource.Id = %q, want %q", got, "roadmap")
				}
				if got := auth.request.GetAction().GetName(); got != proto.App_Invoke_FullMethodName {
					t.Fatalf("Action.Name = %q, want %q", got, proto.App_Invoke_FullMethodName)
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
			name:       "workflow deliver event fills context subject",
			fullMethod: proto.Workflow_DeliverEvent_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			req:        &proto.DeliverWorkflowProviderEventRequest{AppName: "roadmap", Event: &proto.WorkflowEvent{}},
			checkAdapted: func(t *testing.T, adapted gproto.Message) {
				t.Helper()
				out, ok := adapted.(*proto.DeliverWorkflowProviderEventRequest)
				if !ok {
					t.Fatalf("adapted type = %T, want *proto.DeliverWorkflowProviderEventRequest", adapted)
				}
				if out.GetContext().GetSubject().GetId() != "user:alice" {
					t.Fatalf("context.subject.id = %q, want %q", out.GetContext().GetSubject().GetId(), "user:alice")
				}
			},
			checkAuth: func(t *testing.T, auth *stubAuthorizationProvider) {
				t.Helper()
				if got := auth.request.GetResource().GetId(); got != "roadmap" {
					t.Fatalf("Resource.Id = %q, want %q", got, "roadmap")
				}
			},
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
			fullMethod: proto.App_Invoke_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			req:        &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"},
			wantCode:   codes.PermissionDenied,
			setup: func(transport *ProviderGatewayTransport) {
				transport.SetAuthorizationProvider(nil)
			},
		},
		{
			name:       "authorization denied",
			fullMethod: proto.App_Invoke_FullMethodName,
			withOrigin: true,
			introspect: activeAlice,
			authAllow:  &denied,
			req:        &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"},
			wantCode:   codes.PermissionDenied,
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
			if p.SubjectID != wantSubjectID(tc.wantSubject, "user:alice") {
				t.Fatalf("SubjectID = %q, want %q", p.SubjectID, wantSubjectID(tc.wantSubject, "user:alice"))
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
