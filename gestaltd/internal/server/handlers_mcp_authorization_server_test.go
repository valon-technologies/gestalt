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

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	cryptoutil "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/services/apps/oauth"
)

func TestMCPOAuthAccessTokenComesFromProviderTokenExchange(t *testing.T) {
	t.Parallel()

	var lastExchange *core.TokenRequest
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
		TokenFn: func(_ context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
			if req == nil {
				return nil, fmt.Errorf("token request is required")
			}
			if req.GrantType != core.GrantTypeTokenExchange {
				return nil, fmt.Errorf("unexpected grant_type %q", req.GrantType)
			}
			copied := *req
			lastExchange = &copied
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
		auth:        auth,
		encryptor:   enc,
		publicBaseURL: "http://example.test",
		now:         func() time.Time { return now },
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
