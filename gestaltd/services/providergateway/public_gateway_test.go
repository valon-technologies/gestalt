package providergateway

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	testAppInvokeFullMethod          = "/gestalt.provider.v1.App/Invoke"
	testAgentCreateSessionFullMethod = "/gestalt.provider.v1.Agent/CreateSession"
)

func TestPreparePublicRequestAppInvokeFillsContext(t *testing.T) {
	t.Parallel()

	gateway := newTestPublicGateway(t, map[string]publicrpc.PublicMethodPolicy{
		testAppInvokeFullMethod: {
			Fill:   []string{"context"},
			Reject: []string{"run_as"},
		},
	}, &stubIdentityProvider{
		introspect: &core.IntrospectResponse{Active: true, Subject: "user:alice"},
	}, &stubAuthorizationProvider{})

	ctx := publicrpc.WithPublicOrigin(context.Background(), testAppInvokeFullMethod)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer test-token"))
	req := &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"}

	p, adapted, err := gateway.PreparePublicRequest(ctx, testAppInvokeFullMethod, req)
	if err != nil {
		t.Fatalf("PreparePublicRequest: %v", err)
	}
	if p.SubjectID != "user:alice" {
		t.Fatalf("SubjectID = %q, want %q", p.SubjectID, "user:alice")
	}
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
}

func TestPreparePublicRequestAppInvokeRejectsRunAs(t *testing.T) {
	t.Parallel()

	gateway := newTestPublicGateway(t, map[string]publicrpc.PublicMethodPolicy{
		testAppInvokeFullMethod: {Reject: []string{"run_as"}},
	}, &stubIdentityProvider{
		introspect: &core.IntrospectResponse{Active: true, Subject: "user:alice"},
	}, &stubAuthorizationProvider{})

	ctx := publicrpc.WithPublicOrigin(context.Background(), testAppInvokeFullMethod)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer test-token"))
	req := &proto.AppInvokeRequest{
		App:       "roadmap",
		Operation: "sync",
		RunAs:     &proto.SubjectContext{Id: "user:bob"},
	}

	_, _, err := gateway.PreparePublicRequest(ctx, testAppInvokeFullMethod, req)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestPreparePublicRequestAppInvokeRejectsClientContext(t *testing.T) {
	t.Parallel()

	gateway := newTestPublicGateway(t, map[string]publicrpc.PublicMethodPolicy{
		testAppInvokeFullMethod: {Fill: []string{"context"}},
	}, &stubIdentityProvider{
		introspect: &core.IntrospectResponse{Active: true, Subject: "user:alice"},
	}, &stubAuthorizationProvider{})

	ctx := publicrpc.WithPublicOrigin(context.Background(), testAppInvokeFullMethod)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer test-token"))
	req := &proto.AppInvokeRequest{
		App:       "roadmap",
		Operation: "sync",
		Context:   &proto.RequestContext{Subject: &proto.SubjectContext{Id: "user:bob"}},
	}

	_, _, err := gateway.PreparePublicRequest(ctx, testAppInvokeFullMethod, req)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestPreparePublicRequestAgentCreateSessionFillsSubjectFields(t *testing.T) {
	t.Parallel()

	gateway := newTestPublicGateway(t, map[string]publicrpc.PublicMethodPolicy{
		testAgentCreateSessionFullMethod: {
			Fill: []string{"context", "subject", "created_by_subject_id"},
		},
	}, &stubIdentityProvider{
		introspect: &core.IntrospectResponse{Active: true, Subject: "user:alice"},
	}, &stubAuthorizationProvider{})

	ctx := publicrpc.WithPublicOrigin(context.Background(), testAgentCreateSessionFullMethod)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer test-token"))
	req := &proto.CreateAgentProviderSessionRequest{Model: "gpt-test"}

	_, adapted, err := gateway.PreparePublicRequest(ctx, testAgentCreateSessionFullMethod, req)
	if err != nil {
		t.Fatalf("PreparePublicRequest: %v", err)
	}
	out, ok := adapted.(*proto.CreateAgentProviderSessionRequest)
	if !ok {
		t.Fatalf("adapted type = %T, want *proto.CreateAgentProviderSessionRequest", adapted)
	}
	if out.GetSubject().GetId() != "user:alice" {
		t.Fatalf("subject = %#v, want user:alice", out.GetSubject())
	}
	if out.GetCreatedBySubjectId() != "user:alice" {
		t.Fatalf("created_by_subject_id = %q, want %q", out.GetCreatedBySubjectId(), "user:alice")
	}
	if out.GetContext() == nil || out.GetContext().GetSubject().GetId() != "user:alice" {
		t.Fatalf("context subject = %#v, want user:alice", out.GetContext())
	}
}

func TestPreparePublicRequestAuthorizationDenied(t *testing.T) {
	t.Parallel()

	denied := false
	gateway := newTestPublicGateway(t, map[string]publicrpc.PublicMethodPolicy{
		testAppInvokeFullMethod: {},
	}, &stubIdentityProvider{
		introspect: &core.IntrospectResponse{Active: true, Subject: "user:alice"},
	}, &stubAuthorizationProvider{allowedResult: &denied})

	ctx := publicrpc.WithPublicOrigin(context.Background(), testAppInvokeFullMethod)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer test-token"))
	req := &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"}

	_, _, err := gateway.PreparePublicRequest(ctx, testAppInvokeFullMethod, req)
	assertGRPCCode(t, err, codes.PermissionDenied)
}

func TestPreparePublicRequestIdentityFailure(t *testing.T) {
	t.Parallel()

	gateway := newTestPublicGateway(t, map[string]publicrpc.PublicMethodPolicy{
		testAppInvokeFullMethod: {},
	}, &stubIdentityProvider{
		introspect: &core.IntrospectResponse{Active: false},
	}, &stubAuthorizationProvider{})

	ctx := publicrpc.WithPublicOrigin(context.Background(), testAppInvokeFullMethod)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer test-token"))
	req := &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"}

	_, _, err := gateway.PreparePublicRequest(ctx, testAppInvokeFullMethod, req)
	assertGRPCCode(t, err, codes.Unauthenticated)
}

func TestPreparePublicRequestRequiresPublicOrigin(t *testing.T) {
	t.Parallel()

	gateway := newTestPublicGateway(t, map[string]publicrpc.PublicMethodPolicy{
		testAppInvokeFullMethod: {},
	}, &stubIdentityProvider{
		introspect: &core.IntrospectResponse{Active: true, Subject: "user:alice"},
	}, &stubAuthorizationProvider{})

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer test-token"))
	req := &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"}

	_, _, err := gateway.PreparePublicRequest(ctx, testAppInvokeFullMethod, req)
	assertGRPCCode(t, err, codes.Internal)
}

func TestPreparePublicRequestAuthorizationMapping(t *testing.T) {
	t.Parallel()

	authorization := &stubAuthorizationProvider{}
	gateway := newTestPublicGateway(t, map[string]publicrpc.PublicMethodPolicy{
		testAppInvokeFullMethod: {},
	}, &stubIdentityProvider{
		introspect: &core.IntrospectResponse{Active: true, Subject: "user:alice"},
	}, authorization)

	ctx := publicrpc.WithPublicOrigin(context.Background(), testAppInvokeFullMethod)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer test-token"))
	req := &proto.AppInvokeRequest{App: "roadmap", Operation: "sync"}

	if _, _, err := gateway.PreparePublicRequest(ctx, testAppInvokeFullMethod, req); err != nil {
		t.Fatalf("PreparePublicRequest: %v", err)
	}
	if !authorization.called {
		t.Fatal("authorization provider was not called")
	}
	if got := authorization.request.GetSubject().GetId(); got != "user:alice" {
		t.Fatalf("Subject.Id = %q, want %q", got, "user:alice")
	}
	if got := authorization.request.GetResource().GetType(); got != "provider" {
		t.Fatalf("Resource.Type = %q, want %q", got, "provider")
	}
	if got := authorization.request.GetResource().GetId(); got != "roadmap" {
		t.Fatalf("Resource.Id = %q, want %q", got, "roadmap")
	}
	if got := authorization.request.GetAction().GetName(); got != testAppInvokeFullMethod {
		t.Fatalf("Action.Name = %q, want %q", got, testAppInvokeFullMethod)
	}
}

func newTestPublicGateway(
	t *testing.T,
	policies map[string]publicrpc.PublicMethodPolicy,
	identity *stubIdentityProvider,
	authorization *stubAuthorizationProvider,
) *ProviderGatewayTransport {
	t.Helper()
	gateway := NewProviderGatewayTransport()
	gateway.SetPublicMethods(publicrpc.NewMapRegistry(policies))
	gateway.SetIdentity(identity)
	gateway.SetAuthorizationProvider(authorization)
	gateway.SetPublicBaseURL("https://gestalt.example")
	gateway.SetPublicCallerApp("gestaltd")
	return gateway
}

type stubIdentityProvider struct {
	introspect    *core.IntrospectResponse
	introspectErr error
	userInfo      *core.UserInfoResponse
}

func (p *stubIdentityProvider) Authorize(context.Context, *core.AuthorizeRequest) (*core.AuthorizeResponse, error) {
	return &core.AuthorizeResponse{}, nil
}

func (p *stubIdentityProvider) Token(context.Context, *core.TokenRequest) (*core.TokenResponse, error) {
	return &core.TokenResponse{}, nil
}

func (p *stubIdentityProvider) Introspect(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
	if p.introspectErr != nil {
		return nil, p.introspectErr
	}
	if p.introspect == nil {
		return &core.IntrospectResponse{}, nil
	}
	return p.introspect, nil
}

func (p *stubIdentityProvider) ListGrants(context.Context, *core.ListGrantsRequest) (*core.ListGrantsResponse, error) {
	return &core.ListGrantsResponse{}, nil
}

func (p *stubIdentityProvider) GetGrant(context.Context, *core.GetGrantRequest) (*core.GetGrantResponse, error) {
	return &core.GetGrantResponse{}, nil
}

func (p *stubIdentityProvider) RevokeGrant(context.Context, *core.RevokeGrantRequest) (*core.RevokeGrantResponse, error) {
	return &core.RevokeGrantResponse{}, nil
}

func (p *stubIdentityProvider) UserInfo(context.Context, *core.UserInfoRequest) (*core.UserInfoResponse, error) {
	if p.userInfo == nil {
		return nil, core.ErrNotFound
	}
	return p.userInfo, nil
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
