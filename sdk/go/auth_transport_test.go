package gestalt_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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

func (p *fullAuthenticationProvider) SessionTTL() time.Duration {
	return 30 * time.Minute
}

func (p *fullAuthenticationProvider) BeginAuthentication(_ context.Context, _ *gestalt.BeginAuthenticationRequest) (*gestalt.BeginAuthenticationResponse, error) {
	return &gestalt.BeginAuthenticationResponse{
		AuthorizationUrl: "https://auth.example.test/login",
		ProviderState:    []byte("state-data"),
	}, nil
}

func (p *fullAuthenticationProvider) CompleteAuthentication(_ context.Context, _ *gestalt.CompleteAuthenticationRequest) (*gestalt.AuthenticatedUser, error) {
	return testAuthUser(), nil
}

func (p *fullAuthenticationProvider) ValidateExternalToken(_ context.Context, token string) (*gestalt.AuthenticatedUser, error) {
	if token == "valid-token" {
		return testAuthUser(), nil
	}
	return nil, nil
}

func testAuthUser() *gestalt.AuthenticatedUser {
	return &gestalt.AuthenticatedUser{
		Subject:       "user-123",
		Email:         "user@example.test",
		EmailVerified: true,
		DisplayName:   "Test User",
		AvatarUrl:     "https://example.test/avatar.png",
		Claims:        map[string]string{"role": "admin"},
	}
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
	authClient := proto.NewAuthenticationProviderClient(conn)

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

	beginAuthResp, err := authClient.BeginAuthentication(rpcCtx, &proto.BeginAuthenticationRequest{
		CallbackUrl: "https://app.example.test/callback",
		HostState:   "xyz",
		Scopes:      []string{"read", "write"},
		Options:     map[string]string{"prompt": "consent"},
	})
	if err != nil {
		t.Fatalf("BeginAuthentication: %v", err)
	}
	if beginAuthResp.GetAuthorizationUrl() != "https://auth.example.test/login" {
		t.Fatalf("authorization_url = %q, want %q", beginAuthResp.GetAuthorizationUrl(), "https://auth.example.test/login")
	}
	if !bytes.Equal(beginAuthResp.GetProviderState(), []byte("state-data")) {
		t.Fatalf("provider_state = %q, want %q", beginAuthResp.GetProviderState(), "state-data")
	}

	beginResp, err := authClient.BeginLogin(rpcCtx, &proto.BeginLoginRequest{
		CallbackUrl: "https://app.example.test/callback",
		HostState:   "xyz",
		Scopes:      []string{"read", "write"},
		Options:     map[string]string{"prompt": "consent"},
	})
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if beginResp.GetAuthorizationUrl() != "https://auth.example.test/login" {
		t.Fatalf("authorization_url = %q, want %q", beginResp.GetAuthorizationUrl(), "https://auth.example.test/login")
	}
	if !bytes.Equal(beginResp.GetProviderState(), []byte("state-data")) {
		t.Fatalf("provider_state = %q, want %q", beginResp.GetProviderState(), "state-data")
	}

	completeAuthResp, err := authClient.CompleteAuthentication(rpcCtx, &proto.CompleteAuthenticationRequest{
		Query:         map[string]string{"code": "auth-code"},
		ProviderState: []byte("state-data"),
		CallbackUrl:   "https://app.example.test/callback",
	})
	if err != nil {
		t.Fatalf("CompleteAuthentication: %v", err)
	}
	assertAuthenticatedUser(t, completeAuthResp)

	completeResp, err := authClient.CompleteLogin(rpcCtx, &proto.CompleteLoginRequest{
		Query:         map[string]string{"code": "auth-code"},
		ProviderState: []byte("state-data"),
		CallbackUrl:   "https://app.example.test/callback",
	})
	if err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	assertAuthenticatedUser(t, completeResp)

	validAuthUser, err := authClient.Authenticate(rpcCtx, &proto.AuthenticateRequest{
		Input: &proto.AuthenticateRequest_Token{
			Token: &proto.TokenAuthInput{Token: "valid-token"},
		},
	})
	if err != nil {
		t.Fatalf("Authenticate(valid): %v", err)
	}
	assertAuthenticatedUser(t, validAuthUser)

	_, err = authClient.Authenticate(rpcCtx, &proto.AuthenticateRequest{
		Input: &proto.AuthenticateRequest_Http{
			Http: &proto.HTTPRequestAuthInput{
				Method:  "GET",
				Url:     "https://api.example.test/callback",
				Headers: map[string]string{"authorization": "Bearer valid-token"},
			},
		},
	})
	if err == nil {
		t.Fatal("Authenticate(http) should return error")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unimplemented {
		t.Fatalf("Authenticate(http) code = %v, want UNIMPLEMENTED", err)
	}

	validUser, err := authClient.ValidateExternalToken(rpcCtx, &proto.ValidateExternalTokenRequest{Token: "valid-token"})
	if err != nil {
		t.Fatalf("ValidateExternalToken(valid): %v", err)
	}
	assertAuthenticatedUser(t, validUser)

	_, err = authClient.ValidateExternalToken(rpcCtx, &proto.ValidateExternalTokenRequest{Token: "unknown"})
	if err == nil {
		t.Fatal("ValidateExternalToken(unknown) should return error")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("ValidateExternalToken(unknown) code = %v, want NOT_FOUND", err)
	}

	sessionResp, err := authClient.GetSessionSettings(rpcCtx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetSessionSettings: %v", err)
	}
	const expectedTTL int64 = 1800
	if sessionResp.GetSessionTtlSeconds() != expectedTTL {
		t.Fatalf("session_ttl_seconds = %d, want %d", sessionResp.GetSessionTtlSeconds(), expectedTTL)
	}
}

func assertAuthenticatedUser(t *testing.T, user *proto.AuthenticatedUser) {
	t.Helper()
	if user.GetSubject() != "user-123" {
		t.Fatalf("subject = %q, want %q", user.GetSubject(), "user-123")
	}
	if user.GetEmail() != "user@example.test" {
		t.Fatalf("email = %q, want %q", user.GetEmail(), "user@example.test")
	}
	if !user.GetEmailVerified() {
		t.Fatal("email_verified = false, want true")
	}
	if user.GetDisplayName() != "Test User" {
		t.Fatalf("display_name = %q, want %q", user.GetDisplayName(), "Test User")
	}
	if user.GetAvatarUrl() != "https://example.test/avatar.png" {
		t.Fatalf("avatar_url = %q, want %q", user.GetAvatarUrl(), "https://example.test/avatar.png")
	}
	if user.GetClaims()["role"] != "admin" {
		t.Fatalf("claims[role] = %q, want %q", user.GetClaims()["role"], "admin")
	}
}
