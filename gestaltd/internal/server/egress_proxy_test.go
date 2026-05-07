package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestSubjectKindFromID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		subjectID string
		fallback  string
		want      string
	}{
		{"user", "user:nicole@valon.com", "service_account", "user"},
		{"service account", "service_account:nicolebot", "user", "service_account"},
		{"empty falls back", "", "service_account", "service_account"},
		{"missing colon falls back", "nicolebot", "service_account", "service_account"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := subjectKindFromID(tc.subjectID, tc.fallback)
			if got != tc.want {
				t.Errorf("subjectKindFromID(%q, %q) = %q, want %q", tc.subjectID, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestProviderForHost(t *testing.T) {
	t.Parallel()

	s := &Server{
		pluginDefs: map[string]*config.ProviderEntry{
			"bigquery": {Egress: &config.ProviderEgressConfig{
				AllowedHosts: []string{"bigquery.googleapis.com", "*.googleapis.com"},
			}},
			"linear": {Egress: &config.ProviderEgressConfig{
				AllowedHosts: []string{"api.linear.app"},
			}},
			"empty": {Egress: nil},
		},
	}

	cases := []struct {
		host string
		want string
	}{
		{"bigquery.googleapis.com", "bigquery"},
		{"storage.googleapis.com", "bigquery"}, // matched by *.googleapis.com wildcard
		{"api.linear.app", "linear"},
		{"unknown.example.com", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			got := s.providerForHost(tc.host)
			if got != tc.want {
				t.Errorf("providerForHost(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}

// fakeExternalCredentials satisfies core.ExternalCredentialProvider just enough
// to drive egress proxy injection tests.
type fakeExternalCredentials struct {
	stored map[string]string // key = provider+":"+subject -> token
	err    map[string]error  // key = provider+":"+subject -> err to return
	calls  int
}

func (f *fakeExternalCredentials) ResolveCredential(ctx context.Context, req *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
	f.calls++
	key := req.Provider + ":" + req.CredentialSubjectID
	if e, ok := f.err[key]; ok {
		return nil, e
	}
	tok, ok := f.stored[key]
	if !ok {
		return nil, core.ErrNotFound
	}
	return &core.ResolveExternalCredentialResponse{Token: tok}, nil
}

// The remaining ExternalCredentialProvider methods are unused by these tests.
func (f *fakeExternalCredentials) PutCredential(context.Context, *core.ExternalCredential) error {
	return nil
}
func (f *fakeExternalCredentials) RestoreCredential(context.Context, *core.ExternalCredential) error {
	return nil
}
func (f *fakeExternalCredentials) GetCredential(context.Context, string, string, string) (*core.ExternalCredential, error) {
	return nil, nil
}
func (f *fakeExternalCredentials) ListCredentials(context.Context, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}
func (f *fakeExternalCredentials) ListCredentialsForConnection(context.Context, string, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}
func (f *fakeExternalCredentials) DeleteCredential(context.Context, string) error { return nil }
func (f *fakeExternalCredentials) ValidateCredentialConfig(context.Context, *core.ValidateExternalCredentialConfigRequest) error {
	return nil
}
func (f *fakeExternalCredentials) ExchangeCredential(context.Context, *core.ExchangeExternalCredentialRequest) (*core.ExchangeExternalCredentialResponse, error) {
	return nil, nil
}

func newTestImpersonationServer(t *testing.T, fake *fakeExternalCredentials) (*Server, *egressproxy.TokenManager) {
	t.Helper()

	tokens, err := egressproxy.NewTokenManager([]byte("test-secret-32-bytes-aaaaaaaaaaa"))
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	s := &Server{
		egressProxyTokens:   tokens,
		externalCredentials: fake,
		publicBaseURL:       "http://localhost:8080",
		pluginDefs: map[string]*config.ProviderEntry{
			"bigquery": {Egress: &config.ProviderEgressConfig{
				AllowedHosts: []string{"bigquery.googleapis.com"},
			}},
		},
	}
	return s, tokens
}

func TestEgressProxyMiddlewareImpersonationRejectsMissingHeader(t *testing.T) {
	t.Parallel()

	s, tokens := newTestImpersonationServer(t, &fakeExternalCredentials{})
	tok, err := tokens.MintToken(egressproxy.TokenRequest{
		CallerSubjectID: "service_account:nicolebot",
		AllowedHosts:    []string{"bigquery.googleapis.com"},
		MayImpersonate:  true,
		TTL:             time.Hour,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "http://bigquery.googleapis.com/v2/projects", nil)
	r.Header.Set(proxyAuthorizationHeader, "Bearer "+tok)
	w := httptest.NewRecorder()

	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be invoked when impersonation header is missing")
	})
	s.egressProxyMiddleware(notFound).ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "X-On-Behalf-Of") {
		t.Errorf("response = %q, want it to mention X-On-Behalf-Of", w.Body.String())
	}
}

func TestEgressProxyMiddlewareInjectsCredentialOnImpersonation(t *testing.T) {
	t.Parallel()

	// Stand up a fake upstream that records the Authorization header it sees.
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	upstreamHost := upstreamURL.Hostname()

	fake := &fakeExternalCredentials{
		stored: map[string]string{
			"bigquery:user:nicole@valon.com": "user-bq-token-abc123",
		},
	}
	s, tokens := newTestImpersonationServer(t, fake)
	// Override the providerForHost mapping to recognize the test upstream host.
	s.pluginDefs["bigquery"].Egress.AllowedHosts = []string{upstreamHost}

	tok, err := tokens.MintToken(egressproxy.TokenRequest{
		CallerSubjectID: "service_account:nicolebot",
		AllowedHosts:    []string{upstreamHost},
		MayImpersonate:  true,
		TTL:             time.Hour,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, upstream.URL+"/v2/test", nil)
	r.Header.Set(proxyAuthorizationHeader, "Bearer "+tok)
	r.Header.Set(onBehalfOfHeader, "nicole@valon.com")
	r.Header.Set("Authorization", "Bearer client-supplied-bogus")
	w := httptest.NewRecorder()

	notFound := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("inner handler should not be invoked for proxy requests")
	})
	s.egressProxyMiddleware(notFound).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if receivedAuth != "Bearer user-bq-token-abc123" {
		t.Errorf("upstream Authorization = %q, want injected per-user token", receivedAuth)
	}
	if fake.calls != 1 {
		t.Errorf("ResolveCredential calls = %d, want 1", fake.calls)
	}
}

func TestEgressProxyMiddlewareReconnectRequiredSurfaces403(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be reached when reconnect is required")
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	upstreamHost := upstreamURL.Hostname()

	fake := &fakeExternalCredentials{
		err: map[string]error{
			"bigquery:user:nicole@valon.com": core.ErrReconnectRequired,
		},
	}
	s, tokens := newTestImpersonationServer(t, fake)
	s.pluginDefs["bigquery"].Egress.AllowedHosts = []string{upstreamHost}

	tok, _ := tokens.MintToken(egressproxy.TokenRequest{
		CallerSubjectID: "service_account:nicolebot",
		AllowedHosts:    []string{upstreamHost},
		MayImpersonate:  true,
		TTL:             time.Hour,
	})
	r := httptest.NewRequest(http.MethodGet, upstream.URL+"/v2/test", nil)
	r.Header.Set(proxyAuthorizationHeader, "Bearer "+tok)
	r.Header.Set(onBehalfOfHeader, "nicole@valon.com")
	w := httptest.NewRecorder()

	s.egressProxyMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "reconnect_required") {
		t.Errorf("response = %q, want reconnect_required marker", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "/auth/connect?integration=bigquery") {
		t.Errorf("response = %q, want reconnect URL", w.Body.String())
	}
}

func TestEgressProxyMiddlewareNonImpersonatingTokenSkipsRunAs(t *testing.T) {
	t.Parallel()

	var capturedRunAs *core.RunAsSubject
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured := invocation.RunAsAuditFromContext(r.Context())
		capturedRunAs = captured.RunAsSubject
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	upstreamHost := upstreamURL.Hostname()

	s, tokens := newTestImpersonationServer(t, &fakeExternalCredentials{})
	s.pluginDefs["bigquery"].Egress.AllowedHosts = []string{upstreamHost}

	// Mint a NON-impersonating token (MayImpersonate: false).
	tok, _ := tokens.MintToken(egressproxy.TokenRequest{
		PluginName:   "bigquery",
		AllowedHosts: []string{upstreamHost},
		TTL:          time.Hour,
	})
	r := httptest.NewRequest(http.MethodGet, upstream.URL+"/v2/test", nil)
	r.Header.Set(proxyAuthorizationHeader, "Bearer "+tok)
	r.Header.Set(onBehalfOfHeader, "should-be-ignored@valon.com")
	w := httptest.NewRecorder()

	s.egressProxyMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if capturedRunAs != nil {
		t.Errorf("captured RunAsSubject = %+v, want nil for non-impersonating token", capturedRunAs)
	}
}

func TestNormalizeRequestedHostsDeduplicates(t *testing.T) {
	t.Parallel()
	got := normalizeRequestedHosts([]string{"a.com", "  ", "a.com", "B.COM"})
	if len(got) != 2 || got[0] != "a.com" || got[1] != "b.com" {
		t.Errorf("normalizeRequestedHosts = %v, want [a.com b.com]", got)
	}
}

func TestParseRequestedDefaultAction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    egress.PolicyAction
		wantErr bool
	}{
		{"", egress.PolicyAllow, false},
		{"allow", egress.PolicyAllow, false},
		{"DENY", egress.PolicyDeny, false},
		{"bogus", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseRequestedDefaultAction(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseRequestedDefaultAction(%q) err = nil, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Errorf("parseRequestedDefaultAction(%q) err = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseRequestedDefaultAction(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

