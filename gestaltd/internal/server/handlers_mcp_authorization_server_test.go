package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	cryptoutil "github.com/valon-technologies/gestalt/server/core/crypto"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/services/apps/oauth"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMCPOAuthAccessTokenComesFromProviderTokenExchange(t *testing.T) {
	t.Parallel()

	var lastExchange *core.TokenRequest
	var lastCallerSubject string
	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req != nil && req.Token == "subject-token" {
				return &core.IntrospectResponse{
					Active:   true,
					Subject:  "user:test@example.com",
					ClientID: core.DefaultOAuthClientID,
				}, nil
			}
			return &core.IntrospectResponse{Active: false}, nil
		},
		TokenFn: func(ctx context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
			if req == nil {
				return nil, fmt.Errorf("token request is required")
			}
			if req.GrantType != core.GrantTypeTokenExchange {
				return nil, fmt.Errorf("unexpected grant_type %q", req.GrantType)
			}
			copied := *req
			lastExchange = &copied
			lastCallerSubject = gestalt.TrustedCallerSubjectFromContext(ctx)
			return &core.TokenResponse{
				AccessToken: "provider-mcp-access-token",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
				Scope:       req.Scope,
			}, nil
		},
	}

	enc, err := cryptoutil.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	srv := &Server{
		auth:          auth,
		encryptor:     enc,
		publicBaseURL: "http://example.test",
		now:           func() time.Time { return now },
	}

	clientID, err := encodeMCPOAuthClientRegistration(enc, mcpOAuthClientRegistrationState{
		RedirectURIs:            []string{"http://localhost/callback"},
		TokenEndpointAuthMethod: mcpOAuthTokenAuthMethodNone,
		ExpiresAt:               now.Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthClientRegistration() error = %v", err)
	}

	verifier := "mcp-oauth-test-verifier"
	code, err := encodeMCPOAuthAuthorizationCode(enc, mcpOAuthAuthorizationCodeState{
		ClientID:            clientID,
		RedirectURI:         "http://localhost/callback",
		Email:               "test@example.com",
		SubjectToken:        "subject-token",
		CallerSubjectID:     "user:11111111-1111-1111-1111-111111111111",
		CodeChallenge:       oauth.ComputeS256Challenge(verifier),
		CodeChallengeMethod: "S256",
		ExpiresAt:           now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthAuthorizationCode() error = %v", err)
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "http://localhost/callback")
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "example.test"
	rec := httptest.NewRecorder()

	srv.mcpOAuthToken(rec, req)

	if rec.Code != http.StatusOK {
		body, _ := io.ReadAll(rec.Body)
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, body)
	}

	var resp mcpOAuthTokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if resp.AccessToken != "provider-mcp-access-token" {
		t.Fatalf("access_token = %q, want provider-backed token", resp.AccessToken)
	}
	if strings.Count(resp.AccessToken, ".") == 2 {
		t.Fatalf("access_token looks like a host JWT: %q", resp.AccessToken)
	}
	if lastExchange == nil {
		t.Fatal("provider Token() was not called for token exchange")
	}
	if lastExchange.SubjectToken != "subject-token" {
		t.Fatalf("subject_token = %q, want subject-token", lastExchange.SubjectToken)
	}
	if lastCallerSubject != "user:11111111-1111-1111-1111-111111111111" {
		t.Fatalf("caller subject = %q, want canonical caller subject", lastCallerSubject)
	}
}

func TestMCPOAuthAuthorizeStoresCanonicalCallerSubject(t *testing.T) {
	t.Parallel()

	const (
		subjectToken   = "subject-token"
		canonicalOwner = "user:11111111-1111-1111-1111-111111111111"
	)
	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req != nil && req.Token == subjectToken {
				return &core.IntrospectResponse{
					Active:  true,
					Subject: "user:test@example.com",
				}, nil
			}
			return &core.IntrospectResponse{Active: false}, nil
		},
	}
	enc, err := cryptoutil.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	srv := &Server{
		auth:          auth,
		resolver:      principal.NewResolver(auth),
		users:         boundaryUserStore{usersByEmail: map[string]string{"test@example.com": strings.TrimPrefix(canonicalOwner, "user:")}},
		encryptor:     enc,
		publicBaseURL: "http://example.test",
		now:           func() time.Time { return now },
	}

	clientID, err := encodeMCPOAuthClientRegistration(enc, mcpOAuthClientRegistrationState{
		RedirectURIs:            []string{"http://localhost/callback"},
		TokenEndpointAuthMethod: mcpOAuthTokenAuthMethodNone,
		ExpiresAt:               now.Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthClientRegistration() error = %v", err)
	}
	verifier := "mcp-oauth-authorize-verifier"
	query := url.Values{
		"client_id":             []string{clientID},
		"redirect_uri":          []string{"http://localhost/callback"},
		"response_type":         []string{"code"},
		"code_challenge":        []string{oauth.ComputeS256Challenge(verifier)},
		"code_challenge_method": []string{"S256"},
		"state":                 []string{"client-state"},
	}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: subjectToken})
	rec := httptest.NewRecorder()

	srv.mcpOAuthAuthorize(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body = %s", rec.Code, rec.Body.String())
	}
	redirect, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	code, err := decodeMCPOAuthAuthorizationCode(enc, redirect.Query().Get("code"), now)
	if err != nil {
		t.Fatalf("decode authorization code: %v", err)
	}
	if code.CallerSubjectID != canonicalOwner {
		t.Fatalf("caller subject = %q, want %q", code.CallerSubjectID, canonicalOwner)
	}
	if code.SubjectToken != subjectToken {
		t.Fatalf("subject token = %q, want original session token", code.SubjectToken)
	}
}

func TestMCPOAuthRefreshPreservesCallerSubject(t *testing.T) {
	t.Parallel()

	const callerSubject = "user:11111111-1111-1111-1111-111111111111"
	var gotCallerSubject string
	var gotRequest *core.TokenRequest
	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		TokenFn: func(ctx context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
			if req == nil {
				return nil, fmt.Errorf("token request is required")
			}
			copied := *req
			gotRequest = &copied
			gotCallerSubject = gestalt.TrustedCallerSubjectFromContext(ctx)
			return &core.TokenResponse{
				AccessToken: "provider-mcp-refresh-access-token",
				ExpiresIn:   3600,
				Scope:       req.Scope,
			}, nil
		},
	}
	enc, err := cryptoutil.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	srv := &Server{
		auth:          auth,
		encryptor:     enc,
		publicBaseURL: "http://example.test",
		now:           func() time.Time { return now },
	}
	clientID, err := encodeMCPOAuthClientRegistration(enc, mcpOAuthClientRegistrationState{
		RedirectURIs:            []string{"http://localhost/callback"},
		TokenEndpointAuthMethod: mcpOAuthTokenAuthMethodNone,
		ExpiresAt:               now.Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthClientRegistration() error = %v", err)
	}
	refreshToken, err := encodeMCPOAuthRefreshToken(enc, mcpOAuthRefreshTokenState{
		ClientID:        clientID,
		Email:           "test@example.com",
		Scope:           "profile",
		SubjectToken:    "subject-token",
		CallerSubjectID: callerSubject,
		ExpiresAt:       now.Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthRefreshToken() error = %v", err)
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "example.test"
	rec := httptest.NewRecorder()

	srv.mcpOAuthToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if gotCallerSubject != callerSubject {
		t.Fatalf("caller subject = %q, want %q", gotCallerSubject, callerSubject)
	}
	if gotRequest == nil || gotRequest.SubjectToken != "subject-token" {
		t.Fatalf("token request = %+v, want original subject token", gotRequest)
	}
}

func TestMCPOAuthReauthenticatesInactiveSubjectToken(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		TokenFn: func(context.Context, *core.TokenRequest) (*core.TokenResponse, error) {
			return nil, status.Error(codes.Unauthenticated, "subject token is inactive")
		},
	}
	enc, err := cryptoutil.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	srv := &Server{
		auth:          auth,
		encryptor:     enc,
		publicBaseURL: "http://example.test",
		now:           func() time.Time { return now },
	}
	clientID, err := encodeMCPOAuthClientRegistration(enc, mcpOAuthClientRegistrationState{
		RedirectURIs:            []string{"http://localhost/callback"},
		TokenEndpointAuthMethod: mcpOAuthTokenAuthMethodNone,
		ExpiresAt:               now.Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthClientRegistration() error = %v", err)
	}
	verifier := "mcp-oauth-inactive-verifier"
	code, err := encodeMCPOAuthAuthorizationCode(enc, mcpOAuthAuthorizationCodeState{
		ClientID:            clientID,
		RedirectURI:         "http://localhost/callback",
		Email:               "test@example.com",
		SubjectToken:        "inactive-subject-token",
		CallerSubjectID:     "user:11111111-1111-1111-1111-111111111111",
		CodeChallenge:       oauth.ComputeS256Challenge(verifier),
		CodeChallengeMethod: "S256",
		ExpiresAt:           now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthAuthorizationCode() error = %v", err)
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "http://localhost/callback")
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "example.test"
	rec := httptest.NewRecorder()

	srv.mcpOAuthToken(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	var oauthErr mcpOAuthErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&oauthErr); err != nil {
		t.Fatalf("decode OAuth error: %v", err)
	}
	if oauthErr.Error != "invalid_grant" {
		t.Fatalf("error = %q, want invalid_grant", oauthErr.Error)
	}
}

func TestMCPOAuthAccessTokenRejectsMissingSubjectToken(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{N: "mcp-oauth"}
	enc, err := cryptoutil.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	srv := &Server{
		auth:          auth,
		encryptor:     enc,
		publicBaseURL: "http://example.test",
		now:           func() time.Time { return now },
	}

	clientID, err := encodeMCPOAuthClientRegistration(enc, mcpOAuthClientRegistrationState{
		RedirectURIs:            []string{"http://localhost/callback"},
		TokenEndpointAuthMethod: mcpOAuthTokenAuthMethodNone,
		ExpiresAt:               now.Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthClientRegistration() error = %v", err)
	}

	verifier := "mcp-oauth-test-verifier"
	code, err := encodeMCPOAuthAuthorizationCode(enc, mcpOAuthAuthorizationCodeState{
		ClientID:            clientID,
		RedirectURI:         "http://localhost/callback",
		Email:               "test@example.com",
		SubjectToken:        "",
		CallerSubjectID:     "user:11111111-1111-1111-1111-111111111111",
		CodeChallenge:       oauth.ComputeS256Challenge(verifier),
		CodeChallengeMethod: "S256",
		ExpiresAt:           now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthAuthorizationCode() error = %v", err)
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "http://localhost/callback")
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "example.test"
	rec := httptest.NewRecorder()

	srv.mcpOAuthToken(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(rec.Body)
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, body)
	}
}
