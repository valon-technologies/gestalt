package server_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const (
	testSessionToken       = "session-token"
	testGrantSessionCookie = "grant-session-token"
	grantTestScope         = "testapp"
)

func grantTestProviders(t *testing.T) *registry.ProviderMap[core.Provider] {
	t.Helper()
	return testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        grantTestScope,
		ConnMode: core.ConnectionModeNone,
	})
}

func testAuthStubWithIntrospect(fn func(context.Context, string) (*core.IntrospectResponse, error)) *coretesting.StubAuthProvider {
	return &coretesting.StubAuthProvider{
		N: "test",
		IntrospectFn: func(ctx context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req == nil {
				return &core.IntrospectResponse{Active: false}, nil
			}
			return fn(ctx, req.Token)
		},
	}
}

func testIntrospectActive(subjectID, scope string) *core.IntrospectResponse {
	return &core.IntrospectResponse{
		Active:   true,
		Subject:  subjectID,
		Scope:    scope,
		ClientID: core.DefaultOAuthClientID,
	}
}

type grantTrackingAuthStub struct {
	coretesting.StubAuthProvider
	mu                   sync.Mutex
	grants               map[string]*core.GetGrantResponse
	revoked              map[string]struct{}
	pendingScope         string
	pendingState         string
	lastTokenExchangeReq *core.TokenRequest
}

func newGrantTrackingAuthStub() *grantTrackingAuthStub {
	stub := &grantTrackingAuthStub{
		StubAuthProvider: coretesting.StubAuthProvider{N: "grant-stub"},
		grants:           make(map[string]*core.GetGrantResponse),
		revoked:          make(map[string]struct{}),
	}
	stub.AuthorizeFn = func(_ context.Context, req *core.AuthorizeRequest) (*core.AuthorizeResponse, error) {
		if req != nil {
			stub.pendingScope = req.Scope
			stub.pendingState = req.State
		}
		redirect := "https://idp.example.test/login"
		parsed, err := url.Parse(redirect)
		if err != nil {
			return nil, err
		}
		query := parsed.Query()
		if req != nil && req.State != "" {
			query.Set("state", req.State)
		}
		query.Set("code", "stub-auth-code")
		parsed.RawQuery = query.Encode()
		return &core.AuthorizeResponse{RedirectURI: parsed.String()}, nil
	}
	stub.IntrospectFn = func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
		if req == nil || strings.TrimSpace(req.Token) == "" {
			return &core.IntrospectResponse{Active: false}, nil
		}
		switch req.Token {
		case testSessionToken, testGrantSessionCookie:
			return testIntrospectActive("user:test-user", ""), nil
		default:
			if strings.HasPrefix(req.Token, "grant-access-") {
				return testIntrospectActive("user:test-user", ""), nil
			}
			return &core.IntrospectResponse{Active: false}, nil
		}
	}
	stub.TokenFn = func(_ context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
		if req == nil {
			return nil, fmt.Errorf("token request is required")
		}
		grantType := strings.TrimSpace(req.GrantType)
		if grantType == "" {
			grantType = core.GrantTypeAuthorizationCode
		}
		switch grantType {
		case core.GrantTypeTokenExchange:
			if req != nil {
				copied := *req
				stub.lastTokenExchangeReq = &copied
			}
			intro, introErr := stub.IntrospectFn(context.Background(), &core.IntrospectRequest{Token: req.SubjectToken})
			if introErr != nil || intro == nil || !intro.Active {
				return nil, fmt.Errorf("inactive subject token")
			}
			scope := strings.TrimSpace(req.Scope)
			grantID := "grant-exchange-" + strings.TrimPrefix(intro.Subject, "user:")
			if scope != "" {
				grantID = "grant-scoped"
			}
			ttlSeconds := int64(30 * 24 * 3600)
			if req.ExpiresIn > 0 {
				ttlSeconds = req.ExpiresIn
			}
			now := time.Now().UTC()
			stub.mu.Lock()
			stub.grants[grantID] = &core.GetGrantResponse{
				CreatedAt: now.Unix(),
				ExpiresAt: now.Add(time.Duration(ttlSeconds) * time.Second).Unix(),
				Scopes:    scopesFromString(scope),
			}
			stub.mu.Unlock()
			return &core.TokenResponse{
				AccessToken: "grant-access-" + grantID,
				TokenType:   "Bearer",
				ExpiresIn:   int(ttlSeconds),
				GrantID:     grantID,
				Scope:       scope,
			}, nil
		case core.GrantTypeAuthorizationCode:
			if req.Code != "stub-auth-code" {
				return nil, fmt.Errorf("invalid authorization code")
			}
			grantID := "grant-stub"
			scope := strings.TrimSpace(stub.pendingScope)
			now := time.Now().UTC()
			stub.mu.Lock()
			stub.grants[grantID] = &core.GetGrantResponse{
				CreatedAt: now.Unix(),
				ExpiresAt: now.Add(30 * 24 * time.Hour).Unix(),
				Scopes:    scopesFromString(scope),
			}
			stub.mu.Unlock()
			return &core.TokenResponse{
				AccessToken: "grant-access-" + grantID,
				TokenType:   "Bearer",
				ExpiresIn:   30 * 24 * 3600,
				GrantID:     grantID,
				Scope:       scope,
			}, nil
		default:
			return nil, fmt.Errorf("unsupported grant_type %q", grantType)
		}
	}
	stub.ListGrantsFn = func(context.Context, *core.ListGrantsRequest) (*core.ListGrantsResponse, error) {
		stub.mu.Lock()
		defer stub.mu.Unlock()
		ids := make([]string, 0, len(stub.grants))
		for id := range stub.grants {
			if !isManageableAPITokenGrantID(id) {
				continue
			}
			if _, revoked := stub.revoked[id]; revoked {
				continue
			}
			ids = append(ids, id)
		}
		return &core.ListGrantsResponse{GrantIDs: ids}, nil
	}
	stub.GetGrantFn = func(_ context.Context, req *core.GetGrantRequest) (*core.GetGrantResponse, error) {
		if req == nil || req.GrantID == "" {
			return nil, core.ErrNotFound
		}
		if !isManageableAPITokenGrantID(req.GrantID) {
			return nil, core.ErrNotFound
		}
		stub.mu.Lock()
		defer stub.mu.Unlock()
		if _, revoked := stub.revoked[req.GrantID]; revoked {
			return nil, core.ErrNotFound
		}
		grant, ok := stub.grants[req.GrantID]
		if !ok {
			return nil, core.ErrNotFound
		}
		return grant, nil
	}
	stub.RevokeGrantFn = func(_ context.Context, req *core.RevokeGrantRequest) (*core.RevokeGrantResponse, error) {
		if req == nil || req.GrantID == "" {
			return nil, core.ErrNotFound
		}
		if !isManageableAPITokenGrantID(req.GrantID) {
			return nil, core.ErrNotFound
		}
		stub.mu.Lock()
		defer stub.mu.Unlock()
		if _, ok := stub.grants[req.GrantID]; !ok {
			return nil, core.ErrNotFound
		}
		stub.revoked[req.GrantID] = struct{}{}
		return &core.RevokeGrantResponse{}, nil
	}
	return stub
}

func isManageableAPITokenGrantID(id string) bool {
	return id != "grant-stub" && id != "grant-generated"
}

func scopesFromString(scope string) []core.GrantScope {
	parts := principal.ParseScopeString(scope)
	if len(parts) == 0 {
		return nil
	}
	out := make([]core.GrantScope, 0, len(parts))
	for _, part := range parts {
		out = append(out, core.GrantScope{Scope: part})
	}
	return out
}

type hostIssuedSessionAuthOpts struct {
	name        string
	loginHost   string
	email       string
	displayName string
}

func newHostIssuedSessionAuthStub(secret []byte, opts hostIssuedSessionAuthOpts) *coretesting.StubAuthProvider {
	email := opts.email
	if email == "" {
		email = "host@example.com"
	}
	displayName := opts.displayName
	if displayName == "" {
		displayName = "Host Issued"
	}
	name := opts.name
	if name == "" {
		name = "host-issued"
	}
	host := opts.loginHost
	if host == "" {
		host = "idp.example.test"
	}
	identity := &core.UserIdentity{Email: email, DisplayName: displayName}
	return &coretesting.StubAuthProvider{
		N:        name,
		LoginURL: "https://" + host + "/login",
		HandleCallbackFn: func(_ context.Context, code string) (*core.UserIdentity, error) {
			if code != "good-code" {
				return nil, fmt.Errorf("unexpected code %q", code)
			}
			return identity, nil
		},
		TokenFn: func(_ context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
			if req == nil {
				return nil, fmt.Errorf("token request is required")
			}
			grantType := strings.TrimSpace(req.GrantType)
			if grantType == "" {
				grantType = core.GrantTypeAuthorizationCode
			}
			switch grantType {
			case core.GrantTypeTokenExchange:
				claims, err := session.ValidateToken(strings.TrimSpace(req.SubjectToken), secret)
				if err != nil || claims == nil {
					return nil, fmt.Errorf("inactive subject token")
				}
				apiToken, err := session.IssueToken(identity, secret, 30*24*time.Hour)
				if err != nil {
					return nil, err
				}
				return &core.TokenResponse{
					AccessToken: apiToken,
					TokenType:   "Bearer",
					ExpiresIn:   30 * 24 * 3600,
					GrantID:     "grant-host-api",
				}, nil
			case core.GrantTypeAuthorizationCode:
				if req.Code != "good-code" {
					return nil, fmt.Errorf("invalid code")
				}
				token, err := session.IssueToken(identity, secret, time.Hour)
				if err != nil {
					return nil, err
				}
				return &core.TokenResponse{
					AccessToken: token,
					TokenType:   "Bearer",
					ExpiresIn:   3600,
					GrantID:     "grant-host-session",
				}, nil
			default:
				return nil, fmt.Errorf("unsupported grant_type %q", grantType)
			}
		},
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req == nil || strings.TrimSpace(req.Token) == "" {
				return &core.IntrospectResponse{Active: false}, nil
			}
			claims, err := session.ValidateToken(req.Token, secret)
			if err != nil {
				claims = nil
			}
			if claims == nil {
				return &core.IntrospectResponse{Active: false}, nil
			}
			return &core.IntrospectResponse{
				Active:   true,
				Subject:  principal.UserSubjectID("host-user"),
				ClientID: core.DefaultOAuthClientID,
			}, nil
		},
	}
}

type stubAuthWithLoginURL struct {
	coretesting.StubAuthProvider
	loginURL      string
	capturedState string
}

func (s *stubAuthWithLoginURL) Authorize(ctx context.Context, req *core.AuthorizeRequest) (*core.AuthorizeResponse, error) {
	if req != nil {
		s.capturedState = req.State
	}
	redirect := strings.TrimSpace(s.loginURL)
	if redirect == "" {
		redirect = strings.TrimSpace(s.LoginURL)
	}
	if redirect == "" {
		redirect = "https://idp.example.test/login"
	}
	parsed, err := url.Parse(redirect)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	if req != nil && req.State != "" {
		query.Set("state", req.State)
	}
	parsed.RawQuery = query.Encode()
	return &core.AuthorizeResponse{RedirectURI: parsed.String()}, nil
}

func configureGrantTestAuth(cfg *server.Config) {
	cfg.Auth = newGrantTrackingAuthStub()
}

func configureGrantTestAuthForUser(cfg *server.Config, userID string) *grantTrackingAuthStub {
	stub := newGrantTrackingAuthStub()
	subject := principal.UserSubjectID(userID)
	stub.IntrospectFn = func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
		if req == nil || strings.TrimSpace(req.Token) == "" {
			return &core.IntrospectResponse{Active: false}, nil
		}
		switch req.Token {
		case testSessionToken, testGrantSessionCookie:
			return testIntrospectActive(subject, ""), nil
		default:
			if strings.HasPrefix(req.Token, "grant-access-") {
				return testIntrospectActive(subject, ""), nil
			}
			return &core.IntrospectResponse{Active: false}, nil
		}
	}
	cfg.Auth = stub
	return stub
}

func addGrantTestSessionCookie(req *http.Request) {
	req.AddCookie(&http.Cookie{Name: "session_token", Value: testSessionToken})
}

func authStubWithSessionEmail(email string) *coretesting.StubAuthProvider {
	subject := principal.UserSubjectID(strings.ToLower(strings.TrimSpace(email)))
	return testAuthStubWithIntrospect(func(_ context.Context, token string) (*core.IntrospectResponse, error) {
		if token != testSessionToken {
			return &core.IntrospectResponse{Active: false}, nil
		}
		return testIntrospectActive(subject, ""), nil
	})
}

func authStubWithSessionTokenIntrospect(acceptToken, subjectID, scope string) *coretesting.StubAuthProvider {
	return testAuthStubWithIntrospect(func(_ context.Context, token string) (*core.IntrospectResponse, error) {
		if token != acceptToken {
			return &core.IntrospectResponse{Active: false}, nil
		}
		return testIntrospectActive(subjectID, scope), nil
	})
}

func scopedTestBearerToken(userID, scope string) string {
	return fmt.Sprintf("scoped-bearer:%s:%s", userID, scope)
}

func parseScopedTestBearerToken(token string) (userID, scope string, ok bool) {
	if !strings.HasPrefix(token, "scoped-bearer:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(token, "scoped-bearer:")
	userID, scope, ok = strings.Cut(rest, ":")
	return userID, scope, ok
}

func testAuthStubForScopedBearer() *coretesting.StubAuthProvider {
	return testAuthStubWithIntrospect(func(_ context.Context, token string) (*core.IntrospectResponse, error) {
		userID, scope, ok := parseScopedTestBearerToken(token)
		if !ok {
			return &core.IntrospectResponse{Active: false}, nil
		}
		return testIntrospectActive(principal.UserSubjectID(userID), scope), nil
	})
}

type federatedLogoutAuthStub struct {
	coretesting.StubAuthProvider
	logoutPrefix string
}

func (s *federatedLogoutAuthStub) FederatedLogoutURL(returnTo string) (string, error) {
	prefix := strings.TrimSpace(s.logoutPrefix)
	if prefix == "" {
		prefix = "https://idp.example.test/v2/logout"
	}
	return prefix + "?returnTo=" + url.QueryEscape(returnTo), nil
}

func TestLoginCallbackRejectedDomainRedirectsThroughLogout(t *testing.T) {
	t.Parallel()

	stub := &federatedLogoutAuthStub{
		StubAuthProvider: coretesting.StubAuthProvider{N: "auth0"},
	}
	stub.TokenFn = func(_ context.Context, _ *core.TokenRequest) (*core.TokenResponse, error) {
		return nil, fmt.Errorf(`oidc auth: email domain "gmail.com" is not allowed`)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewBufferString(`{"state":"test-state"}`))
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	callbackResp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer func() { _ = callbackResp.Body.Close() }()

	if callbackResp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", callbackResp.StatusCode)
	}
	location := callbackResp.Header.Get("Location")
	if !strings.HasPrefix(location, "https://idp.example.test/v2/logout") {
		t.Fatalf("Location = %q, want federated logout redirect", location)
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	returnTo := parsed.Query().Get("returnTo")
	if !strings.Contains(returnTo, "/api/v1/auth/login/denied") {
		t.Fatalf("returnTo = %q, want login denied page", returnTo)
	}
	if !strings.Contains(returnTo, "domain_not_allowed") {
		t.Fatalf("returnTo = %q, want domain_not_allowed reason", returnTo)
	}

	deniedResp, err := client.Get(ts.URL + "/api/v1/auth/login/denied?reason=domain_not_allowed")
	if err != nil {
		t.Fatalf("denied page: %v", err)
	}
	defer func() { _ = deniedResp.Body.Close() }()
	if deniedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("denied status = %d, want 401", deniedResp.StatusCode)
	}
	body, err := io.ReadAll(deniedResp.Body)
	if err != nil {
		t.Fatalf("read denied body: %v", err)
	}
	if !strings.Contains(string(body), "Try again") || !strings.Contains(string(body), "not allowed") {
		t.Fatalf("denied body = %q, want user-facing denial copy", string(body))
	}
}
