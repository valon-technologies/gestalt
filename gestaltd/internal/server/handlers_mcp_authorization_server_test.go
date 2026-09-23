package server

import (
	"context"
	"encoding/json"
	"errors"
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

func TestMCPOAuthCallerSubjectRejectsNonCanonicalStoredSubject(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	for _, subject := range []string{"user:person@example.com", "user:provider-opaque", "provider:subject"} {
		t.Run(subject, func(t *testing.T) {
			t.Parallel()

			_, err := srv.resolveMCPOAuthCallerSubject(context.Background(), subject, "unused-subject-token")
			if !errors.Is(err, principal.ErrInvalidToken) {
				t.Fatalf("resolveMCPOAuthCallerSubject() error = %v, want invalid token", err)
			}
		})
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
	if rec.Code != http.StatusOK {
		t.Fatalf("authorize status = %d, want 200 (consent page); body = %s", rec.Code, rec.Body.String())
	}
	consentToken := mustExtractConsentToken(t, rec.Body.String())

	consentForm := url.Values{"consent": {consentToken}, "decision": {mcpOAuthConsentDecisionApprove}}
	consentReq := httptest.NewRequest(http.MethodPost, "/oauth/consent", strings.NewReader(consentForm.Encode()))
	consentReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	consentReq.Host = "example.test"
	consentReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: subjectToken})
	consentRec := httptest.NewRecorder()

	srv.mcpOAuthConsentDecision(consentRec, consentReq)
	if consentRec.Code != http.StatusFound {
		t.Fatalf("consent status = %d, want 302; body = %s", consentRec.Code, consentRec.Body.String())
	}
	redirect, err := url.Parse(consentRec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if redirect.Query().Get("state") != "client-state" {
		t.Fatalf("redirect state = %q, want %q", redirect.Query().Get("state"), "client-state")
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

// mustExtractConsentToken pulls the hidden "consent" field's value out of the
// rendered consent page HTML, mirroring what a browser submitting the form
// would send back.
func mustExtractConsentToken(t *testing.T, pageHTML string) string {
	t.Helper()
	const marker = `name="consent" value="`
	start := strings.Index(pageHTML, marker)
	if start == -1 {
		t.Fatalf("consent page did not contain a consent field: %s", pageHTML)
	}
	start += len(marker)
	end := strings.Index(pageHTML[start:], `"`)
	if end == -1 {
		t.Fatalf("consent page had a malformed consent field: %s", pageHTML)
	}
	return pageHTML[start : start+end]
}

func TestMCPOAuthAuthorizeRendersConsentPageWithoutIssuingCode(t *testing.T) {
	t.Parallel()

	const subjectToken = "subject-token"
	const canonicalOwner = "user:11111111-1111-1111-1111-111111111111"
	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req != nil && req.Token == subjectToken {
				return &core.IntrospectResponse{Active: true, Subject: "user:test@example.com"}, nil
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
		ClientName:              "Evil Corp Exfil Tool",
		TokenEndpointAuthMethod: mcpOAuthTokenAuthMethodNone,
		ExpiresAt:               now.Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthClientRegistration() error = %v", err)
	}
	verifier := "mcp-oauth-consent-page-verifier"
	query := url.Values{
		"client_id":             []string{clientID},
		"redirect_uri":          []string{"http://localhost/callback"},
		"response_type":         []string{"code"},
		"code_challenge":        []string{oauth.ComputeS256Challenge(verifier)},
		"code_challenge_method": []string{"S256"},
		"scope":                 []string{"mcp:tools mcp:resources"},
		"state":                 []string{"client-state"},
	}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: subjectToken})
	rec := httptest.NewRecorder()

	srv.mcpOAuthAuthorize(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Evil Corp Exfil Tool") {
		t.Fatalf("consent page did not name the requesting client: %s", body)
	}
	if !strings.Contains(body, "It will have access to: mcp:tools mcp:resources") {
		t.Fatalf("consent page did not accurately describe requested access: %s", body)
	}
	if strings.Contains(body, mcpOAuthAuthorizationCodePrefix) {
		t.Fatalf("consent page leaked an authorization code before approval: %s", body)
	}
}

func TestMCPOAuthConsentWarnsAboutEmptyScope(t *testing.T) {
	t.Parallel()

	if got := mcpOAuthConsentAccessText(""); got != "It will have full MCP access." {
		t.Fatalf("empty scope text = %q, want full MCP access warning", got)
	}
}

func TestMCPOAuthConsentPageDoesNotRestrictFormAction(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	renderMCPOAuthConsentPage(rec, mcpOAuthConsentPageView{})

	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "form-action") {
		t.Fatalf("Content-Security-Policy must not restrict form-action: %s", csp)
	}
	if !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("Content-Security-Policy lost default-src protection: %s", csp)
	}
}

func TestMCPOAuthConsentDenialRedirectsWithAccessDenied(t *testing.T) {
	t.Parallel()

	const (
		subjectToken   = "subject-token"
		canonicalOwner = "user:11111111-1111-1111-1111-111111111111"
	)
	enc, err := cryptoutil.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req != nil && req.Token == subjectToken {
				return &core.IntrospectResponse{Active: true, Subject: "user:test@example.com"}, nil
			}
			return &core.IntrospectResponse{Active: false}, nil
		},
	}
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
	consent, err := encodeMCPOAuthConsent(enc, mcpOAuthConsentState{
		ClientID:            clientID,
		RedirectURI:         "http://localhost/callback",
		Email:               "test@example.com",
		SubjectToken:        subjectToken,
		CallerSubjectID:     canonicalOwner,
		CodeChallenge:       oauth.ComputeS256Challenge("verifier"),
		CodeChallengeMethod: "S256",
		OAuthState:          "client-state",
		ExpiresAt:           now.Add(mcpOAuthConsentTTL).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthConsent() error = %v", err)
	}

	form := url.Values{"consent": {consent}, "decision": {mcpOAuthConsentDecisionDeny}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/consent", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: subjectToken})
	rec := httptest.NewRecorder()

	srv.mcpOAuthConsentDecision(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body = %s", rec.Code, rec.Body.String())
	}
	redirect, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if redirect.Query().Get("error") != "access_denied" {
		t.Fatalf("redirect error = %q, want access_denied; full redirect = %s", redirect.Query().Get("error"), redirect)
	}
	if redirect.Query().Get("code") != "" {
		t.Fatalf("denied consent must not include a code; redirect = %s", redirect)
	}
}

func TestMCPOAuthConsentRejectsSessionMismatch(t *testing.T) {
	t.Parallel()

	const (
		grantedToken = "granted-subject-token"
		otherToken   = "other-subject-token"
		grantedEmail = "granted@example.com"
		otherEmail   = "other@example.com"
		grantedOwner = "user:11111111-1111-1111-1111-111111111111"
		otherOwner   = "user:22222222-2222-2222-2222-222222222222"
	)
	enc, err := cryptoutil.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			switch {
			case req != nil && req.Token == grantedToken:
				return &core.IntrospectResponse{Active: true, Subject: "user:" + grantedEmail}, nil
			case req != nil && req.Token == otherToken:
				return &core.IntrospectResponse{Active: true, Subject: "user:" + otherEmail}, nil
			default:
				return &core.IntrospectResponse{Active: false}, nil
			}
		},
	}
	srv := &Server{
		auth:     auth,
		resolver: principal.NewResolver(auth),
		users: boundaryUserStore{usersByEmail: map[string]string{
			grantedEmail: strings.TrimPrefix(grantedOwner, "user:"),
			otherEmail:   strings.TrimPrefix(otherOwner, "user:"),
		}},
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
	consent, err := encodeMCPOAuthConsent(enc, mcpOAuthConsentState{
		ClientID:            clientID,
		RedirectURI:         "http://localhost/callback",
		Email:               "granted@example.com",
		SubjectToken:        grantedToken,
		CallerSubjectID:     grantedOwner,
		CodeChallenge:       oauth.ComputeS256Challenge("verifier"),
		CodeChallengeMethod: "S256",
		OAuthState:          "client-state",
		ExpiresAt:           now.Add(mcpOAuthConsentTTL).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthConsent() error = %v", err)
	}

	// A different session (e.g. the browser logged out and someone else
	// logged in) tries to approve the pending consent issued to grantedOwner.
	form := url.Values{"consent": {consent}, "decision": {mcpOAuthConsentDecisionApprove}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/consent", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: otherToken})
	rec := httptest.NewRecorder()

	srv.mcpOAuthConsentDecision(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	var resp mcpOAuthErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Error != "invalid_request" {
		t.Fatalf("error = %q, want invalid_request (session-mismatch rejection, not an unrelated resolution failure)", resp.Error)
	}
}

func TestMCPOAuthAuthorizationCodeUpgradesLegacyCallerSubject(t *testing.T) {
	t.Parallel()

	const canonicalOwner = "user:11111111-1111-1111-1111-111111111111"
	var gotCallerSubject string
	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req != nil && req.Token == "legacy-subject-token" {
				return &core.IntrospectResponse{Active: true, Subject: "user:test@example.com"}, nil
			}
			return &core.IntrospectResponse{Active: false}, nil
		},
		TokenFn: func(ctx context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
			gotCallerSubject = gestalt.TrustedCallerSubjectFromContext(ctx)
			return &core.TokenResponse{AccessToken: "provider-mcp-access-token", ExpiresIn: 3600, Scope: req.Scope}, nil
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
	verifier := "mcp-oauth-legacy-code-verifier"
	code, err := encodeMCPOAuthAuthorizationCode(enc, mcpOAuthAuthorizationCodeState{
		ClientID:            clientID,
		RedirectURI:         "http://localhost/callback",
		Email:               "test@example.com",
		SubjectToken:        "legacy-subject-token",
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
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if gotCallerSubject != canonicalOwner {
		t.Fatalf("caller subject = %q, want %q", gotCallerSubject, canonicalOwner)
	}
	var resp mcpOAuthTokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	rotated, err := decodeMCPOAuthRefreshToken(enc, resp.RefreshToken, now)
	if err != nil {
		t.Fatalf("decode rotated refresh token: %v", err)
	}
	if rotated.CallerSubjectID != canonicalOwner {
		t.Fatalf("rotated caller subject = %q, want %q", rotated.CallerSubjectID, canonicalOwner)
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

func TestMCPOAuthRefreshUpgradesLegacyCallerSubject(t *testing.T) {
	t.Parallel()

	const canonicalOwner = "user:11111111-1111-1111-1111-111111111111"
	var gotCallerSubject string
	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req != nil && req.Token == "legacy-subject-token" {
				return &core.IntrospectResponse{Active: true, Subject: "user:test@example.com"}, nil
			}
			return &core.IntrospectResponse{Active: false}, nil
		},
		TokenFn: func(ctx context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
			gotCallerSubject = gestalt.TrustedCallerSubjectFromContext(ctx)
			return &core.TokenResponse{AccessToken: "provider-mcp-refresh-access-token", ExpiresIn: 3600, Scope: req.Scope}, nil
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
	refreshToken, err := encodeMCPOAuthRefreshToken(enc, mcpOAuthRefreshTokenState{
		ClientID:     clientID,
		Email:        "test@example.com",
		Scope:        "profile",
		SubjectToken: "legacy-subject-token",
		ExpiresAt:    now.Add(24 * time.Hour).Unix(),
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
	if gotCallerSubject != canonicalOwner {
		t.Fatalf("caller subject = %q, want %q", gotCallerSubject, canonicalOwner)
	}
	var resp mcpOAuthTokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	rotated, err := decodeMCPOAuthRefreshToken(enc, resp.RefreshToken, now)
	if err != nil {
		t.Fatalf("decode rotated refresh token: %v", err)
	}
	if rotated.CallerSubjectID != canonicalOwner {
		t.Fatalf("rotated caller subject = %q, want %q", rotated.CallerSubjectID, canonicalOwner)
	}
}

func TestMCPOAuthReauthenticatesLegacyRefreshToken(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		IntrospectFn: func(_ context.Context, _ *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{Active: false}, nil
		},
		TokenFn: func(context.Context, *core.TokenRequest) (*core.TokenResponse, error) {
			t.Fatal("provider token exchange should not run for a legacy refresh token")
			return nil, nil
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
		ClientID:     clientID,
		Email:        "test@example.com",
		SubjectToken: "subject-token",
		ExpiresAt:    now.Add(24 * time.Hour).Unix(),
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
	if oauthErr.ErrorDescription != mcpOAuthReauthorizationDescription {
		t.Fatalf("error_description = %q, want %q", oauthErr.ErrorDescription, mcpOAuthReauthorizationDescription)
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
	if oauthErr.ErrorDescription != mcpOAuthReauthorizationDescription {
		t.Fatalf("error_description = %q, want %q", oauthErr.ErrorDescription, mcpOAuthReauthorizationDescription)
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

// TestMCPOAuthTokenRejectsConsentTokenPresentedAsCode guards against a
// consent token being relabeled as an authorization code: the two states
// share every field an authorization code needs, so without a bound purpose
// swapping the "gst_mcp_consent_" prefix for "gst_mcp_code_" would decode
// successfully and mint a token without ever going through /oauth/consent.
func TestMCPOAuthTokenRejectsConsentTokenPresentedAsCode(t *testing.T) {
	t.Parallel()

	enc, err := cryptoutil.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	srv := &Server{
		auth:          &coretesting.StubAuthProvider{N: "mcp-oauth"},
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
	verifier := "mcp-oauth-relabel-verifier"
	consent, err := encodeMCPOAuthConsent(enc, mcpOAuthConsentState{
		ClientID:            clientID,
		RedirectURI:         "http://localhost/callback",
		Email:               "victim@example.com",
		SubjectToken:        "victim-subject-token",
		CallerSubjectID:     "user:11111111-1111-1111-1111-111111111111",
		CodeChallenge:       oauth.ComputeS256Challenge(verifier),
		CodeChallengeMethod: "S256",
		OAuthState:          "client-state",
		ExpiresAt:           now.Add(mcpOAuthConsentTTL).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthConsent() error = %v", err)
	}
	relabeled := mcpOAuthAuthorizationCodePrefix + strings.TrimPrefix(consent, mcpOAuthConsentPrefix)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", relabeled)
	form.Set("redirect_uri", "http://localhost/callback")
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "example.test"
	rec := httptest.NewRecorder()

	srv.mcpOAuthToken(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (relabeled consent token must not decode as a code); body = %s", rec.Code, rec.Body.String())
	}
}

func TestMCPOAuthAuthorizePromptNoneReturnsConsentRequired(t *testing.T) {
	t.Parallel()

	const subjectToken = "subject-token"
	const canonicalOwner = "user:11111111-1111-1111-1111-111111111111"
	auth := &coretesting.StubAuthProvider{
		N: "mcp-oauth",
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req != nil && req.Token == subjectToken {
				return &core.IntrospectResponse{Active: true, Subject: "user:test@example.com"}, nil
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
	verifier := "mcp-oauth-prompt-none-verifier"
	query := url.Values{
		"client_id":             []string{clientID},
		"redirect_uri":          []string{"http://localhost/callback"},
		"response_type":         []string{"code"},
		"code_challenge":        []string{oauth.ComputeS256Challenge(verifier)},
		"code_challenge_method": []string{"S256"},
		"prompt":                []string{"none"},
		"state":                 []string{"client-state"},
	}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: subjectToken})
	rec := httptest.NewRecorder()

	srv.mcpOAuthAuthorize(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (prompt=none must not render an interactive page); body = %s", rec.Code, rec.Body.String())
	}
	redirect, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if redirect.Query().Get("error") != "consent_required" {
		t.Fatalf("redirect error = %q, want consent_required; full redirect = %s", redirect.Query().Get("error"), redirect)
	}
}

func TestMCPOAuthAcceptsLegacyEncryptedClientRegistration(t *testing.T) {
	t.Parallel()

	enc, err := cryptoutil.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	legacy := mcpOAuthClientRegistrationState{
		RedirectURIs:            []string{"http://localhost/callback"},
		TokenEndpointAuthMethod: mcpOAuthTokenAuthMethodNone,
		ExpiresAt:               now.Add(24 * time.Hour).Unix(),
	}
	encoded, err := encodeEncryptedState(enc, "mcp oauth client registration", legacy)
	if err != nil {
		t.Fatalf("encodeEncryptedState() error = %v", err)
	}
	if _, err := decodeMCPOAuthClientRegistration(enc, mcpOAuthClientIDPrefix+encoded, now); err != nil {
		t.Fatalf("decode legacy client registration: %v", err)
	}
}

func TestMCPOAuthAcceptsLegacyEncryptedAuthorizationCodeAndRefreshToken(t *testing.T) {
	t.Parallel()

	enc, err := cryptoutil.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	clientID, err := encodeMCPOAuthClientRegistration(enc, mcpOAuthClientRegistrationState{
		RedirectURIs:            []string{"http://localhost/callback"},
		TokenEndpointAuthMethod: mcpOAuthTokenAuthMethodNone,
		ExpiresAt:               now.Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeMCPOAuthClientRegistration() error = %v", err)
	}

	legacyCode := mcpOAuthAuthorizationCodeState{
		ClientID:            clientID,
		RedirectURI:         "http://localhost/callback",
		Email:               "test@example.com",
		SubjectToken:        "subject-token",
		CallerSubjectID:     "user:11111111-1111-1111-1111-111111111111",
		CodeChallenge:       oauth.ComputeS256Challenge("verifier"),
		CodeChallengeMethod: "S256",
		ExpiresAt:           now.Add(time.Hour).Unix(),
	}
	encodedCode, err := encodeEncryptedState(enc, "mcp oauth authorization code", legacyCode)
	if err != nil {
		t.Fatalf("encode legacy code: %v", err)
	}
	if _, err := decodeMCPOAuthAuthorizationCode(enc, mcpOAuthAuthorizationCodePrefix+encodedCode, now); err != nil {
		t.Fatalf("decode legacy authorization code: %v", err)
	}

	legacyRefresh := mcpOAuthRefreshTokenState{
		ClientID:        clientID,
		Email:           "test@example.com",
		SubjectToken:    "subject-token",
		CallerSubjectID: "user:11111111-1111-1111-1111-111111111111",
		ExpiresAt:       now.Add(24 * time.Hour).Unix(),
	}
	encodedRefresh, err := encodeEncryptedState(enc, "mcp oauth refresh token", legacyRefresh)
	if err != nil {
		t.Fatalf("encode legacy refresh token: %v", err)
	}
	if _, err := decodeMCPOAuthRefreshToken(enc, mcpOAuthRefreshTokenPrefix+encodedRefresh, now); err != nil {
		t.Fatalf("decode legacy refresh token: %v", err)
	}
}
