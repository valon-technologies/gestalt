package gestalt_test

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fullDatastoreProvider struct {
	closeTracker
	mu        sync.Mutex
	migrated  bool
	nextID    int
	users     map[string]*gestalt.StoredUser
	tokens    map[string]*gestalt.StoredIntegrationToken
	apiTokens map[string]*gestalt.StoredAPIToken
	oauthRegs map[string]*gestalt.OAuthRegistration
}

func newFullDatastoreProvider() *fullDatastoreProvider {
	return &fullDatastoreProvider{
		users:     make(map[string]*gestalt.StoredUser),
		tokens:    make(map[string]*gestalt.StoredIntegrationToken),
		apiTokens: make(map[string]*gestalt.StoredAPIToken),
		oauthRegs: make(map[string]*gestalt.OAuthRegistration),
	}
}

func (p *fullDatastoreProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (p *fullDatastoreProvider) HealthCheck(context.Context) error {
	return nil
}

func (p *fullDatastoreProvider) Migrate(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.migrated = true
	return nil
}

func (p *fullDatastoreProvider) GetUser(_ context.Context, id string) (*gestalt.StoredUser, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	user, ok := p.users[id]
	if !ok {
		return nil, nil
	}
	return user, nil
}

func (p *fullDatastoreProvider) FindOrCreateUser(_ context.Context, email string) (*gestalt.StoredUser, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range p.users {
		if u.Email == email {
			return u, nil
		}
	}
	p.nextID++
	now := time.Now().UTC().Truncate(time.Second)
	user := &gestalt.StoredUser{
		ID:          fmt.Sprintf("user-%d", p.nextID),
		Email:       email,
		DisplayName: email,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	p.users[user.ID] = user
	return user, nil
}

func (p *fullDatastoreProvider) PutIntegrationToken(_ context.Context, token *gestalt.StoredIntegrationToken) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if token.ID == "" {
		p.nextID++
		token.ID = fmt.Sprintf("itoken-%d", p.nextID)
	}
	p.tokens[token.ID] = token
	return nil
}

func (p *fullDatastoreProvider) GetIntegrationToken(_ context.Context, userID, integration, connection, instance string) (*gestalt.StoredIntegrationToken, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.tokens {
		if t.UserID == userID && t.Integration == integration && t.Connection == connection && t.Instance == instance {
			return t, nil
		}
	}
	return nil, nil
}

func (p *fullDatastoreProvider) ListIntegrationTokens(_ context.Context, userID, integration, connection string) ([]*gestalt.StoredIntegrationToken, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var result []*gestalt.StoredIntegrationToken
	for _, t := range p.tokens {
		if t.UserID == userID && t.Integration == integration && t.Connection == connection {
			result = append(result, t)
		}
	}
	return result, nil
}

func (p *fullDatastoreProvider) DeleteIntegrationToken(_ context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.tokens, id)
	return nil
}

func (p *fullDatastoreProvider) PutAPIToken(_ context.Context, token *gestalt.StoredAPIToken) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if token.ID == "" {
		p.nextID++
		token.ID = fmt.Sprintf("apitoken-%d", p.nextID)
	}
	p.apiTokens[token.ID] = token
	return nil
}

func (p *fullDatastoreProvider) GetAPITokenByHash(_ context.Context, hashedToken string) (*gestalt.StoredAPIToken, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.apiTokens {
		if t.HashedToken == hashedToken {
			return t, nil
		}
	}
	return nil, nil
}

func (p *fullDatastoreProvider) ListAPITokens(_ context.Context, userID string) ([]*gestalt.StoredAPIToken, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var result []*gestalt.StoredAPIToken
	for _, t := range p.apiTokens {
		if t.UserID == userID {
			result = append(result, t)
		}
	}
	return result, nil
}

func (p *fullDatastoreProvider) RevokeAPIToken(_ context.Context, userID, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.apiTokens[id]; ok && t.UserID == userID {
		delete(p.apiTokens, id)
	}
	return nil
}

func (p *fullDatastoreProvider) RevokeAllAPITokens(_ context.Context, userID string) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var count int64
	for id, t := range p.apiTokens {
		if t.UserID == userID {
			delete(p.apiTokens, id)
			count++
		}
	}
	return count, nil
}

func (p *fullDatastoreProvider) GetOAuthRegistration(_ context.Context, authServerURL, redirectURI string) (*gestalt.OAuthRegistration, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reg, ok := p.oauthRegs[authServerURL+"|"+redirectURI]
	if !ok {
		return nil, nil
	}
	return reg, nil
}

func (p *fullDatastoreProvider) PutOAuthRegistration(_ context.Context, registration *gestalt.OAuthRegistration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.oauthRegs[registration.AuthServerURL+"|"+registration.RedirectURI] = registration
	return nil
}

func (p *fullDatastoreProvider) DeleteOAuthRegistration(_ context.Context, authServerURL, redirectURI string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.oauthRegs, authServerURL+"|"+redirectURI)
	return nil
}

func TestDatastoreProviderRoundTrip(t *testing.T) {
	socket := newSocketPath(t, "datastore.sock")
	t.Setenv(proto.EnvPluginSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	provider := newFullDatastoreProvider()
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeDatastoreProvider(ctx, provider)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	runtimeClient := proto.NewPluginRuntimeClient(conn)
	dsClient := proto.NewDatastorePluginClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()

	_, err := dsClient.Migrate(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !provider.migrated {
		t.Fatal("migrated = false, want true")
	}

	createResp, err := dsClient.FindOrCreateUser(rpcCtx, &proto.FindOrCreateUserRequest{Email: "alice@example.test"})
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	userID := createResp.GetId()
	if userID == "" {
		t.Fatal("FindOrCreateUser returned empty ID")
	}
	if createResp.GetEmail() != "alice@example.test" {
		t.Fatalf("email = %q, want %q", createResp.GetEmail(), "alice@example.test")
	}

	getResp, err := dsClient.GetUser(rpcCtx, &proto.GetUserRequest{Id: userID})
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if getResp.GetEmail() != "alice@example.test" {
		t.Fatalf("GetUser email = %q, want %q", getResp.GetEmail(), "alice@example.test")
	}

	_, err = dsClient.GetUser(rpcCtx, &proto.GetUserRequest{Id: "nonexistent"})
	if err == nil {
		t.Fatal("GetUser(nonexistent) should return error")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("GetUser(nonexistent) code = %v, want NOT_FOUND", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(time.Hour)
	lastRefreshed := now.Add(-10 * time.Minute)
	_, err = dsClient.PutStoredIntegrationToken(rpcCtx, &proto.StoredIntegrationToken{
		Id:                 "tok-1",
		UserId:             userID,
		Integration:        "widget-svc",
		Connection:         "oauth2",
		Instance:           "primary",
		AccessTokenSealed:  []byte("sealed-access"),
		RefreshTokenSealed: []byte("sealed-refresh"),
		Scopes:             "read write",
		ExpiresAt:          timestamppb.New(expiresAt),
		LastRefreshedAt:    timestamppb.New(lastRefreshed),
		RefreshErrorCount:  2,
		ConnectionParams:   map[string]string{"domain": "example.test"},
		CreatedAt:          timestamppb.New(now),
		UpdatedAt:          timestamppb.New(now),
	})
	if err != nil {
		t.Fatalf("PutStoredIntegrationToken: %v", err)
	}

	gotToken, err := dsClient.GetStoredIntegrationToken(rpcCtx, &proto.GetStoredIntegrationTokenRequest{
		UserId:      userID,
		Integration: "widget-svc",
		Connection:  "oauth2",
		Instance:    "primary",
	})
	if err != nil {
		t.Fatalf("GetStoredIntegrationToken: %v", err)
	}
	if gotToken.GetId() != "tok-1" {
		t.Fatalf("token id = %q, want %q", gotToken.GetId(), "tok-1")
	}
	if gotToken.GetIntegration() != "widget-svc" {
		t.Fatalf("integration = %q, want %q", gotToken.GetIntegration(), "widget-svc")
	}
	if !bytes.Equal(gotToken.GetAccessTokenSealed(), []byte("sealed-access")) {
		t.Fatalf("access_token_sealed mismatch")
	}
	if !bytes.Equal(gotToken.GetRefreshTokenSealed(), []byte("sealed-refresh")) {
		t.Fatalf("refresh_token_sealed mismatch")
	}
	if gotToken.GetScopes() != "read write" {
		t.Fatalf("scopes = %q, want %q", gotToken.GetScopes(), "read write")
	}
	if gotToken.GetExpiresAt().AsTime().Unix() != expiresAt.Unix() {
		t.Fatalf("expires_at = %v, want %v", gotToken.GetExpiresAt().AsTime(), expiresAt)
	}
	if gotToken.GetLastRefreshedAt().AsTime().Unix() != lastRefreshed.Unix() {
		t.Fatalf("last_refreshed_at = %v, want %v", gotToken.GetLastRefreshedAt().AsTime(), lastRefreshed)
	}
	if gotToken.GetRefreshErrorCount() != 2 {
		t.Fatalf("refresh_error_count = %d, want 2", gotToken.GetRefreshErrorCount())
	}
	if gotToken.GetConnectionParams()["domain"] != "example.test" {
		t.Fatalf("connection_params[domain] = %q, want %q", gotToken.GetConnectionParams()["domain"], "example.test")
	}

	listResp, err := dsClient.ListStoredIntegrationTokens(rpcCtx, &proto.ListStoredIntegrationTokensRequest{
		UserId:      userID,
		Integration: "widget-svc",
		Connection:  "oauth2",
	})
	if err != nil {
		t.Fatalf("ListStoredIntegrationTokens: %v", err)
	}
	if len(listResp.GetTokens()) != 1 {
		t.Fatalf("list tokens count = %d, want 1", len(listResp.GetTokens()))
	}

	_, err = dsClient.DeleteStoredIntegrationToken(rpcCtx, &proto.DeleteStoredIntegrationTokenRequest{Id: "tok-1"})
	if err != nil {
		t.Fatalf("DeleteStoredIntegrationToken: %v", err)
	}

	listResp, err = dsClient.ListStoredIntegrationTokens(rpcCtx, &proto.ListStoredIntegrationTokensRequest{
		UserId:      userID,
		Integration: "widget-svc",
		Connection:  "oauth2",
	})
	if err != nil {
		t.Fatalf("ListStoredIntegrationTokens (empty): %v", err)
	}
	if len(listResp.GetTokens()) != 0 {
		t.Fatalf("list tokens count after delete = %d, want 0", len(listResp.GetTokens()))
	}

	apiExpiry := now.Add(24 * time.Hour)
	_, err = dsClient.PutAPIToken(rpcCtx, &proto.StoredAPIToken{
		Id:          "api-1",
		UserId:      userID,
		Name:        "test-key",
		HashedToken: "sha256-abc",
		Scopes:      "admin",
		ExpiresAt:   timestamppb.New(apiExpiry),
		CreatedAt:   timestamppb.New(now),
		UpdatedAt:   timestamppb.New(now),
	})
	if err != nil {
		t.Fatalf("PutAPIToken: %v", err)
	}

	gotAPI, err := dsClient.GetAPITokenByHash(rpcCtx, &proto.GetAPITokenByHashRequest{HashedToken: "sha256-abc"})
	if err != nil {
		t.Fatalf("GetAPITokenByHash: %v", err)
	}
	if gotAPI.GetId() != "api-1" {
		t.Fatalf("api token id = %q, want %q", gotAPI.GetId(), "api-1")
	}
	if gotAPI.GetName() != "test-key" {
		t.Fatalf("api token name = %q, want %q", gotAPI.GetName(), "test-key")
	}

	apiList, err := dsClient.ListAPITokens(rpcCtx, &proto.ListAPITokensRequest{UserId: userID})
	if err != nil {
		t.Fatalf("ListAPITokens: %v", err)
	}
	if len(apiList.GetTokens()) != 1 {
		t.Fatalf("api tokens count = %d, want 1", len(apiList.GetTokens()))
	}

	_, err = dsClient.RevokeAPIToken(rpcCtx, &proto.RevokeAPITokenRequest{UserId: userID, Id: "api-1"})
	if err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}

	_, err = dsClient.GetAPITokenByHash(rpcCtx, &proto.GetAPITokenByHashRequest{HashedToken: "sha256-abc"})
	if err == nil {
		t.Fatal("GetAPITokenByHash after revoke should return error")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("GetAPITokenByHash after revoke code = %v, want NOT_FOUND", err)
	}

	_, err = dsClient.PutAPIToken(rpcCtx, &proto.StoredAPIToken{
		Id:          "api-2",
		UserId:      userID,
		Name:        "bulk-key",
		HashedToken: "sha256-def",
		Scopes:      "read",
		CreatedAt:   timestamppb.New(now),
		UpdatedAt:   timestamppb.New(now),
	})
	if err != nil {
		t.Fatalf("PutAPIToken (bulk): %v", err)
	}
	revokeResp, err := dsClient.RevokeAllAPITokens(rpcCtx, &proto.RevokeAllAPITokensRequest{UserId: userID})
	if err != nil {
		t.Fatalf("RevokeAllAPITokens: %v", err)
	}
	if revokeResp.GetRevoked() != 1 {
		t.Fatalf("revoked = %d, want 1", revokeResp.GetRevoked())
	}
	apiList, err = dsClient.ListAPITokens(rpcCtx, &proto.ListAPITokensRequest{UserId: userID})
	if err != nil {
		t.Fatalf("ListAPITokens after revoke all: %v", err)
	}
	if len(apiList.GetTokens()) != 0 {
		t.Fatalf("api tokens after revoke all = %d, want 0", len(apiList.GetTokens()))
	}

	discoveredAt := now.Add(-time.Hour)
	oauthExpiry := now.Add(30 * 24 * time.Hour)
	_, err = dsClient.PutOAuthRegistration(rpcCtx, &proto.OAuthRegistration{
		AuthServerUrl:         "https://idp.example.test",
		RedirectUri:           "https://app.example.test/callback",
		ClientId:              "client-xyz",
		ClientSecretSealed:    []byte("sealed-secret"),
		ExpiresAt:             timestamppb.New(oauthExpiry),
		AuthorizationEndpoint: "https://idp.example.test/authorize",
		TokenEndpoint:         "https://idp.example.test/token",
		ScopesSupported:       "openid profile",
		DiscoveredAt:          timestamppb.New(discoveredAt),
	})
	if err != nil {
		t.Fatalf("PutOAuthRegistration: %v", err)
	}

	gotReg, err := dsClient.GetOAuthRegistration(rpcCtx, &proto.GetOAuthRegistrationRequest{
		AuthServerUrl: "https://idp.example.test",
		RedirectUri:   "https://app.example.test/callback",
	})
	if err != nil {
		t.Fatalf("GetOAuthRegistration: %v", err)
	}
	if gotReg.GetClientId() != "client-xyz" {
		t.Fatalf("client_id = %q, want %q", gotReg.GetClientId(), "client-xyz")
	}
	if !bytes.Equal(gotReg.GetClientSecretSealed(), []byte("sealed-secret")) {
		t.Fatalf("client_secret_sealed mismatch")
	}
	if gotReg.GetAuthorizationEndpoint() != "https://idp.example.test/authorize" {
		t.Fatalf("authorization_endpoint = %q, want %q", gotReg.GetAuthorizationEndpoint(), "https://idp.example.test/authorize")
	}
	if gotReg.GetScopesSupported() != "openid profile" {
		t.Fatalf("scopes_supported = %q, want %q", gotReg.GetScopesSupported(), "openid profile")
	}

	_, err = dsClient.DeleteOAuthRegistration(rpcCtx, &proto.DeleteOAuthRegistrationRequest{
		AuthServerUrl: "https://idp.example.test",
		RedirectUri:   "https://app.example.test/callback",
	})
	if err != nil {
		t.Fatalf("DeleteOAuthRegistration: %v", err)
	}

	_, err = dsClient.GetOAuthRegistration(rpcCtx, &proto.GetOAuthRegistrationRequest{
		AuthServerUrl: "https://idp.example.test",
		RedirectUri:   "https://app.example.test/callback",
	})
	if err == nil {
		t.Fatal("GetOAuthRegistration after delete should return error")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("GetOAuthRegistration after delete code = %v, want NOT_FOUND", err)
	}

	health, err := runtimeClient.HealthCheck(rpcCtx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !health.GetReady() {
		t.Fatalf("ready = false, want true")
	}
}
