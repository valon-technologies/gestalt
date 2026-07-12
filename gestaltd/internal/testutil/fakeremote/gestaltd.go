package fakeremote

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
	identityservice "github.com/valon-technologies/gestalt/server/services/identity"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

// InvokeCall records one app invocation received by a fake remote gestaltd.
type InvokeCall struct {
	Provider  string
	Operation string
}

// RecordingInvoker records app invocations routed through the fake remote gestaltd.
type RecordingInvoker struct {
	mu    sync.Mutex
	calls []InvokeCall
}

type recordingInvoker struct {
	delegate invocation.Invoker
	records  *RecordingInvoker
}

func (r *recordingInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	r.records.mu.Lock()
	r.records.calls = append(r.records.calls, InvokeCall{Provider: providerName, Operation: operation})
	r.records.mu.Unlock()
	return r.delegate.Invoke(ctx, p, providerName, instance, operation, params)
}

func (r *RecordingInvoker) Snapshot() []InvokeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]InvokeCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// BearerRecorder stores bearer tokens observed by the fake remote identity provider.
type BearerRecorder struct {
	mu     sync.Mutex
	tokens []string
}

func (b *BearerRecorder) Record(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = append(b.tokens, token)
}

func (b *BearerRecorder) Snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.tokens))
	copy(out, b.tokens)
	return out
}

type allowAllAuthorization struct{}

func (allowAllAuthorization) CheckAccess(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	return &proto.CheckAccessResponse{Allowed: true}, nil
}

func (allowAllAuthorization) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{}, nil
}

func (allowAllAuthorization) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func (allowAllAuthorization) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (allowAllAuthorization) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (allowAllAuthorization) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}

func (allowAllAuthorization) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (allowAllAuthorization) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}

func (allowAllAuthorization) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}

func (allowAllAuthorization) Ping(context.Context) error { return nil }

func (allowAllAuthorization) Close() error { return nil }

type countingAgentProvider struct {
	coreagent.UnimplementedProvider
	sessions *int
}

func (p countingAgentProvider) CreateSession(context.Context, *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	if p.sessions != nil {
		*p.sessions++
	}
	return &coreagent.Session{ID: "remote-session", ProviderName: "managed"}, nil
}

type stubAgentControl struct {
	provider coreagent.Provider
}

func (s stubAgentControl) ResolveProvider(context.Context, string) (string, coreagent.Provider, error) {
	return "managed", s.provider, nil
}

func (s stubAgentControl) ProviderNames() []string { return []string{"managed"} }

// Gestaltd is a fake remote gestaltd exposing generated public gRPC APIs.
type Gestaltd struct {
	URL            string
	Token          string
	Clients        *remote.ClientSet
	Invoker        *RecordingInvoker
	BearerRecorder *BearerRecorder
	agentSessions  *int
	close          func()
}

// AgentSessions returns how many agent sessions the fake remote gestaltd created.
func (g *Gestaltd) AgentSessions() int {
	if g == nil || g.agentSessions == nil {
		return 0
	}
	return *g.agentSessions
}

// AppStub returns a catalog-backed app provider for the fake remote gestaltd.
func AppStub(name, operation string) *coretesting.StubIntegration {
	return &coretesting.StubIntegration{
		N:        name,
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name:       name,
			Operations: []catalog.CatalogOperation{{ID: operation}},
		},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: 202, Body: []byte("relayed")}, nil
		},
	}
}

// Start boots a TLS fake remote gestaltd with public App and Agent gRPC surfaces.
func Start(t *testing.T, remoteApps ...*coretesting.StubIntegration) *Gestaltd {
	t.Helper()

	records := &RecordingInvoker{}
	bearerRecorder := &BearerRecorder{}
	auth := scopedBearerAuth(bearerRecorder)
	authz := allowAllAuthorization{}

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("publicrpc.NewGeneratedRegistry: %v", err)
	}
	transport := providergateway.NewProviderGatewayTransport()
	transport.SetIdentityProvider(auth)
	transport.SetPublicMethods(registry)
	transport.SetAuthorizationProvider(authz)
	transport.SetPublicBaseURL("https://gestalt.test")

	providers := make([]core.Provider, 0, len(remoteApps))
	for _, app := range remoteApps {
		providers = append(providers, app)
	}
	providerRegistry := testutil.NewProviderRegistry(t, providers...)
	services := testutil.NewStubServices(t)
	broker := invocation.NewBroker(providerRegistry, services.Users, services.ExternalCredentials)
	invoker := &recordingInvoker{delegate: broker, records: records}

	var agentSessions int
	agentManager := agentmanager.New(agentmanager.Config{
		Agent:     stubAgentControl{provider: countingAgentProvider{sessions: &agentSessions}},
		Invoker:   broker,
		Providers: providerRegistry,
	})

	handler := buildPublicGRPCHandler(publicGRPCConfig{
		Transport:      transport,
		Invoker:        invoker,
		AgentManager:   agentManager,
		Authentication: auth,
		Authorization:  authz,
	})
	if handler == nil {
		t.Fatal("buildPublicGRPCHandler returned nil")
	}

	ts := httptest.NewUnstartedServer(handler)
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)

	token := ScopedBearerToken("remote-user", "")
	clients, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:       ts.URL,
		Token:     token,
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only fake remote
	})
	if err != nil {
		t.Fatalf("remote.NewClientSet: %v", err)
	}
	t.Cleanup(func() { _ = clients.Close() })

	return &Gestaltd{
		URL:            ts.URL,
		Token:          token,
		Clients:        clients,
		Invoker:        records,
		BearerRecorder: bearerRecorder,
		agentSessions:  &agentSessions,
		close:          ts.Close,
	}
}

type publicGRPCConfig struct {
	Transport      *providergateway.ProviderGatewayTransport
	Invoker        invocation.Invoker
	AgentManager   agentmanager.Service
	Authentication core.IdentityProvider
	Authorization  core.AuthorizationProvider
}

func buildPublicGRPCHandler(cfg publicGRPCConfig) http.Handler {
	if cfg.Transport == nil || cfg.Invoker == nil {
		return nil
	}
	srv := grpc.NewServer(grpc.UnaryInterceptor(publicPrepareUnaryInterceptor(cfg.Transport)))
	publicrpc.RegisterPublicAppServer(srv, appaccessservice.NewServer(
		cfg.Invoker,
		appaccessservice.WithAgentAppInvocationAuthorizer(cfg.AgentManager),
	))
	if cfg.AgentManager != nil {
		publicrpc.RegisterPublicAgentServer(srv, agentservice.NewProviderServer(
			"gestaltd",
			cfg.AgentManager,
		))
	}
	if cfg.Authentication != nil {
		publicrpc.RegisterPublicIdentityServer(srv, identityservice.NewProviderServer(cfg.Authentication))
	}
	if cfg.Authorization != nil {
		publicrpc.RegisterPublicAuthorizationServer(srv, authorizationservice.NewProviderServer(cfg.Authorization))
	}
	return http.HandlerFunc(srv.ServeHTTP)
}

func publicPrepareUnaryInterceptor(transport *providergateway.ProviderGatewayTransport) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		origin, ok := publicrpc.PublicOriginFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "bearer token is required")
		}
		msg, ok := req.(gproto.Message)
		if !ok {
			return nil, status.Error(codes.Internal, "request type mismatch")
		}
		p, adapted, err := transport.PreparePublicRequest(ctx, origin.FullMethod, msg)
		if err != nil {
			return nil, err
		}
		if p != nil {
			ctx = principal.WithPrincipal(ctx, principal.Canonicalized(p))
		}
		return handler(ctx, adapted)
	}
}

func scopedBearerAuth(recorder *BearerRecorder) *coretesting.StubAuthProvider {
	return &coretesting.StubAuthProvider{
		N: "scoped-bearer",
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req == nil {
				return &core.IntrospectResponse{Active: false}, nil
			}
			recorder.Record(req.Token)
			userID, scope, ok := parseScopedBearerToken(req.Token)
			if !ok {
				return &core.IntrospectResponse{Active: false}, nil
			}
			resp := &core.IntrospectResponse{
				Active:   true,
				Subject:  principal.UserSubjectID(userID),
				ClientID: core.DefaultOAuthClientID,
			}
			if scope != "" {
				resp.Scope = scope
			}
			return resp, nil
		},
	}
}

// ScopedBearerToken builds a bearer token accepted by Start's identity provider.
func ScopedBearerToken(userID, scope string) string {
	return fmt.Sprintf("scoped-bearer:%s:%s", userID, scope)
}

func parseScopedBearerToken(token string) (userID, scope string, ok bool) {
	if !strings.HasPrefix(token, "scoped-bearer:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(token, "scoped-bearer:")
	userID, scope, ok = strings.Cut(rest, ":")
	return userID, scope, ok
}
