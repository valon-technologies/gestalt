package gestalt_test

import (
	"context"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type configCall struct {
	name   string
	config map[string]any
}

type fullAuthenticationProvider struct {
	closeTracker
	configured []configCall
	started    int
	revoked    []string
}

func (p *fullAuthenticationProvider) Configure(_ context.Context, name string, config map[string]any) error {
	p.configured = append(p.configured, configCall{name: name, config: config})
	return nil
}

func (p *fullAuthenticationProvider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindAuthentication,
		Name:        "stub-auth",
		DisplayName: "Stub Auth",
		Version:     "1.0",
	}
}

func (p *fullAuthenticationProvider) Warnings() []string {
	return []string{"battery low"}
}

func (p *fullAuthenticationProvider) HealthCheck(context.Context) error {
	return nil
}

func (p *fullAuthenticationProvider) Start(context.Context) error {
	p.started++
	return nil
}

func (p *fullAuthenticationProvider) Authorize(_ context.Context, req *gestalt.AuthorizeRequest) (*gestalt.AuthorizeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	return &gestalt.AuthorizeResponse{
		RedirectURI: "https://auth.example.test/login?state=" + req.State,
	}, nil
}

func (p *fullAuthenticationProvider) Token(_ context.Context, req *gestalt.TokenRequest) (*gestalt.TokenResponse, error) {
	if req == nil || req.Code != "auth-code" {
		return nil, status.Error(codes.InvalidArgument, "invalid authorization code")
	}
	return &gestalt.TokenResponse{
		AccessToken: "issued-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   1800,
		GrantID:     "grant-1",
		Scope:       "read write",
	}, nil
}

func (p *fullAuthenticationProvider) Introspect(_ context.Context, req *gestalt.IntrospectRequest) (*gestalt.IntrospectResponse, error) {
	if req == nil {
		return &gestalt.IntrospectResponse{Active: false}, nil
	}
	switch req.Token {
	case "issued-access-token", "valid-token":
		return &gestalt.IntrospectResponse{
			Active:   true,
			Subject:  "user:user@example.test",
			Scope:    "read write",
			ClientID: "gestaltd",
		}, nil
	default:
		return &gestalt.IntrospectResponse{Active: false}, nil
	}
}

func (p *fullAuthenticationProvider) UserInfo(ctx context.Context, _ *gestalt.UserInfoRequest) (*gestalt.UserInfoResponse, error) {
	call := gestalt.AuthCallContextFromContext(ctx)
	if call.CallerBearerToken != "valid-token" {
		return nil, status.Error(codes.NotFound, "userinfo not found")
	}
	return &gestalt.UserInfoResponse{
		SubjectID: "user:user@example.test",
		Email:     "user@example.test",
		Name:      "Test User",
	}, nil
}

func (p *fullAuthenticationProvider) ListGrants(_ context.Context, _ *gestalt.ListGrantsRequest) (*gestalt.ListGrantsResponse, error) {
	return &gestalt.ListGrantsResponse{GrantIDs: []string{"grant-1", "grant-2"}}, nil
}

func (p *fullAuthenticationProvider) GetGrant(_ context.Context, req *gestalt.GetGrantRequest) (*gestalt.GetGrantResponse, error) {
	if req == nil || req.GrantID != "grant-1" {
		return nil, status.Error(codes.NotFound, "grant not found")
	}
	return &gestalt.GetGrantResponse{
		Scopes: []gestalt.GrantScope{
			{Scope: "read"},
			{Scope: "write"},
		},
		CreatedAt: 1_700_000_000,
		ExpiresAt: 1_800_000_000,
	}, nil
}

func (p *fullAuthenticationProvider) RevokeGrant(_ context.Context, req *gestalt.RevokeGrantRequest) (*gestalt.RevokeGrantResponse, error) {
	if req == nil || req.GrantID == "" {
		return nil, status.Error(codes.InvalidArgument, "grant_id is required")
	}
	p.revoked = append(p.revoked, req.GrantID)
	return &gestalt.RevokeGrantResponse{}, nil
}

func TestAuthenticationProviderRoundTrip(t *testing.T) {
	socket := newSocketPath(t, "auth.sock")
	t.Setenv(proto.EnvProviderSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	provider := &fullAuthenticationProvider{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeAuthenticationProvider(ctx, provider)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	runtimeClient := proto.NewProviderLifecycleClient(conn)
	authClient := proto.NewAuthenticationClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()

	meta, err := runtimeClient.GetProviderIdentity(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetProviderIdentity: %v", err)
	}
	if meta.GetKind() != proto.ProviderKind_PROVIDER_KIND_AUTHENTICATION {
		t.Fatalf("kind = %v, want AUTHENTICATION", meta.GetKind())
	}
	if meta.GetName() != "stub-auth" {
		t.Fatalf("name = %q, want %q", meta.GetName(), "stub-auth")
	}
	if meta.GetVersion() != "1.0" {
		t.Fatalf("version = %q, want %q", meta.GetVersion(), "1.0")
	}
	if meta.GetMinProtocolVersion() != proto.CurrentProtocolVersion {
		t.Fatalf("min_protocol_version = %d, want %d", meta.GetMinProtocolVersion(), proto.CurrentProtocolVersion)
	}
	if meta.GetMaxProtocolVersion() != proto.CurrentProtocolVersion {
		t.Fatalf("max_protocol_version = %d, want %d", meta.GetMaxProtocolVersion(), proto.CurrentProtocolVersion)
	}
	found := false
	for _, w := range meta.GetWarnings() {
		if w == "battery low" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want to contain %q", meta.GetWarnings(), "battery low")
	}

	cfg, _ := structpb.NewStruct(map[string]any{"clientId": "abc"})
	configuredResp, err := runtimeClient.ConfigureProvider(rpcCtx, &proto.ConfigureProviderRequest{
		Name:            "my-auth",
		Config:          cfg,
		ProtocolVersion: proto.CurrentProtocolVersion,
	})
	if err != nil {
		t.Fatalf("ConfigureProvider: %v", err)
	}
	if configuredResp.GetProtocolVersion() != proto.CurrentProtocolVersion {
		t.Fatalf("configured protocol_version = %d, want %d", configuredResp.GetProtocolVersion(), proto.CurrentProtocolVersion)
	}
	if len(provider.configured) != 1 {
		t.Fatalf("configured calls = %d, want 1", len(provider.configured))
	}
	if provider.configured[0].name != "my-auth" {
		t.Fatalf("configured name = %q, want %q", provider.configured[0].name, "my-auth")
	}
	if provider.configured[0].config["clientId"] != "abc" {
		t.Fatalf("configured config[clientId] = %v, want %q", provider.configured[0].config["clientId"], "abc")
	}
	if provider.started != 0 {
		t.Fatalf("started after configure = %d, want 0", provider.started)
	}

	startedResp, err := runtimeClient.StartProvider(rpcCtx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("StartProvider: %v", err)
	}
	if startedResp.GetProtocolVersion() != proto.CurrentProtocolVersion {
		t.Fatalf("started protocol_version = %d, want %d", startedResp.GetProtocolVersion(), proto.CurrentProtocolVersion)
	}
	if provider.started != 1 {
		t.Fatalf("started calls = %d, want 1", provider.started)
	}

	_, err = runtimeClient.ConfigureProvider(rpcCtx, &proto.ConfigureProviderRequest{
		Name:            "my-auth",
		Config:          cfg,
		ProtocolVersion: proto.CurrentProtocolVersion + 1,
	})
	if err == nil {
		t.Fatal("ConfigureProvider should fail for mismatched protocol version")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.FailedPrecondition {
		t.Fatalf("ConfigureProvider code = %v, want FAILED_PRECONDITION", err)
	}
	if len(provider.configured) != 1 {
		t.Fatalf("configured calls = %d after mismatch, want 1", len(provider.configured))
	}

	health, err := runtimeClient.HealthCheck(rpcCtx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !health.GetReady() {
		t.Fatalf("ready = false, want true")
	}

	authorizeResp, err := authClient.Authorize(rpcCtx, &proto.AuthorizeRequest{
		ResponseType: "code",
		ClientId:     "gestaltd",
		RedirectUri:  "https://app.example.test/callback",
		Scope:        "read write",
		State:        "xyz",
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if authorizeResp.GetRedirectUri() != "https://auth.example.test/login?state=xyz" {
		t.Fatalf("redirect_uri = %q, want %q", authorizeResp.GetRedirectUri(), "https://auth.example.test/login?state=xyz")
	}

	tokenResp, err := authClient.Token(rpcCtx, &proto.TokenRequest{
		GrantType:   "authorization_code",
		Code:        "auth-code",
		RedirectUri: "https://app.example.test/callback",
		ClientId:    "gestaltd",
	})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tokenResp.GetAccessToken() != "issued-access-token" {
		t.Fatalf("access_token = %q, want %q", tokenResp.GetAccessToken(), "issued-access-token")
	}
	if tokenResp.GetGrantId() != "grant-1" {
		t.Fatalf("grant_id = %q, want %q", tokenResp.GetGrantId(), "grant-1")
	}

	introspectResp, err := authClient.Introspect(rpcCtx, &proto.IntrospectRequest{Token: "valid-token"})
	if err != nil {
		t.Fatalf("Introspect(valid): %v", err)
	}
	assertIntrospection(t, introspectResp)

	inactiveResp, err := authClient.Introspect(rpcCtx, &proto.IntrospectRequest{Token: "unknown"})
	if err != nil {
		t.Fatalf("Introspect(unknown): %v", err)
	}
	if inactiveResp.GetActive() {
		t.Fatal("introspect(unknown).active = true, want false")
	}

	grantCtx := metadata.AppendToOutgoingContext(rpcCtx, gestalt.CallerBearerTokenMetadataKey, "valid-token")
	listResp, err := authClient.ListGrants(grantCtx, &proto.ListGrantsRequest{})
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(listResp.GetGrantIds()) != 2 {
		t.Fatalf("grant_ids = %v, want 2 entries", listResp.GetGrantIds())
	}

	getResp, err := authClient.GetGrant(grantCtx, &proto.GetGrantRequest{GrantId: "grant-1"})
	if err != nil {
		t.Fatalf("GetGrant: %v", err)
	}
	if len(getResp.GetScopes()) != 2 {
		t.Fatalf("scopes = %d, want 2", len(getResp.GetScopes()))
	}

	userInfoResp, err := authClient.UserInfo(grantCtx, &proto.UserInfoRequest{})
	if err != nil {
		t.Fatalf("UserInfo: %v", err)
	}
	if userInfoResp.GetSubjectId() != "user:user@example.test" {
		t.Fatalf("userinfo subject_id = %q, want %q", userInfoResp.GetSubjectId(), "user:user@example.test")
	}
	if userInfoResp.GetName() != "Test User" {
		t.Fatalf("userinfo name = %q, want %q", userInfoResp.GetName(), "Test User")
	}

	_, err = authClient.RevokeGrant(grantCtx, &proto.RevokeGrantRequest{GrantId: "grant-1"})
	if err != nil {
		t.Fatalf("RevokeGrant: %v", err)
	}
	if len(provider.revoked) != 1 || provider.revoked[0] != "grant-1" {
		t.Fatalf("revoked = %v, want [grant-1]", provider.revoked)
	}
}

func assertIntrospection(t *testing.T, resp *proto.IntrospectResponse) {
	t.Helper()
	if !resp.GetActive() {
		t.Fatal("active = false, want true")
	}
	if resp.GetSubject() != "user:user@example.test" {
		t.Fatalf("subject = %q, want %q", resp.GetSubject(), "user:user@example.test")
	}
	if resp.GetScope() != "read write" {
		t.Fatalf("scope = %q, want %q", resp.GetScope(), "read write")
	}
	if resp.GetClientId() != "gestaltd" {
		t.Fatalf("client_id = %q, want %q", resp.GetClientId(), "gestaltd")
	}
}
