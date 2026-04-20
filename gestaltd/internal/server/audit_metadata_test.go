package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAuditMetadata_IPAndUserAgent(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "audit-prov",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: http.MethodPost}},
	}

	providers := testutil.NewProviderRegistry(t, stub)
	svc := coretesting.NewStubServices(t)
	broker := invocation.NewBroker(providers, svc.Users, svc.Tokens)
	guarded := invocation.NewGuarded(broker, broker, "http", auditSink, invocation.WithoutRateLimit())

	srv, err := server.New(server.Config{
		Auth: &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "session@example.com"}, nil
			},
		},
		AuditSink:   auditSink,
		Services:    svc,
		Providers:   providers,
		Invoker:     guarded,
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/audit-prov/ping", bytes.NewBufferString(`{}`))
	req.Header.Set("X-Forwarded-For", "203.0.113.42, 10.0.0.1")
	req.Header.Set("User-Agent", "test-client/1.0")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var record map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse audit JSON: %v\nraw: %s", err, auditBuf.String())
	}

	if record["client_ip"] != "203.0.113.42" {
		t.Errorf("expected client_ip=203.0.113.42, got %v", record["client_ip"])
	}
	remoteAddr, _ := record["remote_addr"].(string)
	if remoteAddr == "" {
		t.Error("expected non-empty remote_addr for direct connection address")
	}
	if remoteAddr == "203.0.113.42" {
		t.Error("remote_addr should be the actual connection address, not the XFF value")
	}
	if record["user_agent"] != "test-client/1.0" {
		t.Errorf("expected user_agent=test-client/1.0, got %v", record["user_agent"])
	}
	if record["provider"] != "audit-prov" {
		t.Errorf("expected provider=audit-prov, got %v", record["provider"])
	}
	if record["auth_source"] != "session" {
		t.Errorf("expected auth_source=session, got %v", record["auth_source"])
	}
	if uid, ok := record["user_id"].(string); !ok || uid == "" {
		t.Errorf("expected non-empty user_id, got %v", record["user_id"])
	}
	if record["allowed"] != true {
		t.Errorf("expected allowed=true, got %v", record["allowed"])
	}
}

func TestAuditMetadata_FallbackToRemoteAddr(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "audit-fallback-prov",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: http.MethodPost}},
	}

	providers := testutil.NewProviderRegistry(t, stub)
	svc := coretesting.NewStubServices(t)
	broker := invocation.NewBroker(providers, svc.Users, svc.Tokens)
	guarded := invocation.NewGuarded(broker, broker, "http", auditSink, invocation.WithoutRateLimit())

	srv, err := server.New(server.Config{
		Auth:        &coretesting.StubAuthProvider{N: "none"},
		Services:    svc,
		Providers:   providers,
		Invoker:     guarded,
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/audit-fallback-prov/ping", bytes.NewBufferString(`{}`))
	req.Header.Set("User-Agent", "fallback-agent/2.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var record map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse audit JSON: %v\nraw: %s", err, auditBuf.String())
	}

	clientIP, ok := record["client_ip"].(string)
	if !ok || clientIP == "" {
		t.Errorf("expected non-empty client_ip from RemoteAddr fallback, got %v", record["client_ip"])
	}
	remoteAddr, ok := record["remote_addr"].(string)
	if !ok || remoteAddr == "" {
		t.Errorf("expected non-empty remote_addr, got %v", record["remote_addr"])
	}
	if clientIP != remoteAddr {
		t.Errorf("without XFF, client_ip and remote_addr should match: client_ip=%v, remote_addr=%v", clientIP, remoteAddr)
	}
	if record["user_agent"] != "fallback-agent/2.0" {
		t.Errorf("expected user_agent=fallback-agent/2.0, got %v", record["user_agent"])
	}
}

func TestAuditMetadata_AuthMiddlewareFailures(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		method         string
		path           string
		body           string
		authHeader     string
		withMCPHandler bool
		wantSource     string
		wantError      string
		wantAuthSource string
	}{
		{
			name:       "missing_http_auth",
			method:     http.MethodGet,
			path:       "/api/v1/integrations",
			wantSource: "http",
			wantError:  "missing authorization",
		},
		{
			name:           "missing_mcp_auth",
			method:         http.MethodPost,
			path:           "/mcp",
			body:           `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
			withMCPHandler: true,
			wantSource:     "mcp",
			wantError:      "missing authorization",
		},
		{
			name:           "invalid_session_bearer",
			method:         http.MethodPost,
			path:           "/mcp",
			body:           `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
			authHeader:     "Bearer invalid-session-token",
			withMCPHandler: true,
			wantSource:     "mcp",
			wantError:      "invalid token",
			wantAuthSource: "session",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var auditBuf bytes.Buffer
			auditSink := invocation.NewSlogAuditSink(&auditBuf)

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Auth = &coretesting.StubAuthProvider{
					N: "stub",
					ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
						return nil, fmt.Errorf("invalid token")
					},
				}
				cfg.AuditSink = auditSink
				cfg.Services = coretesting.NewStubServices(t)
				if tc.withMCPHandler {
					cfg.MCPHandler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
						t.Fatal("unexpected MCP handler invocation")
					})
				}
			})
			t.Cleanup(ts.Close)

			var body io.Reader
			if tc.body != "" {
				body = bytes.NewBufferString(tc.body)
			}
			req, _ := http.NewRequest(tc.method, ts.URL+tc.path, body)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.ReadAll(resp.Body)

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", resp.StatusCode)
			}

			var record map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(auditBuf.Bytes()), &record); err != nil {
				t.Fatalf("failed to parse audit JSON: %v\nraw: %s", err, auditBuf.String())
			}

			if record["source"] != tc.wantSource {
				t.Fatalf("expected source %q, got %v", tc.wantSource, record["source"])
			}
			if record["operation"] != "auth.authenticate" {
				t.Fatalf("expected operation auth.authenticate, got %v", record["operation"])
			}
			if record["allowed"] != false {
				t.Fatalf("expected allowed=false, got %v", record["allowed"])
			}
			if record["error"] != tc.wantError {
				t.Fatalf("expected error %q, got %v", tc.wantError, record["error"])
			}
			if record["provider"] != "" {
				t.Fatalf("expected empty provider, got %v", record["provider"])
			}
			if tc.wantAuthSource == "" {
				if authSource, ok := record["auth_source"]; ok && authSource != "" {
					t.Fatalf("expected empty auth_source, got %v", authSource)
				}
			} else if record["auth_source"] != tc.wantAuthSource {
				t.Fatalf("expected auth_source %q, got %v", tc.wantAuthSource, record["auth_source"])
			}
		})
	}
}

func TestAuditMetadata_WorkloadSubjectAndCredentialPath(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)

	workloadToken, _, err := principal.GenerateToken(principal.TokenTypeIdentity)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "audit-workload-prov",
			ConnMode: core.ConnectionModeIdentity,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				if token != "identity-token" {
					t.Fatalf("unexpected token %q", token)
				}
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: http.MethodPost}},
	}

	providers := testutil.NewProviderRegistry(t, stub)
	authz, err := authorization.New(config.AuthorizationConfig{
		IdentityTokens: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: workloadToken,
				Providers: map[string]config.WorkloadProviderDef{
					"audit-workload-prov": {
						Connection: "workspace",
						Instance:   "team-a",
						Allow:      []string{"ping"},
					},
				},
			},
		},
	}, nil, providers, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	if err := svc.Tokens.StoreIdentityToken(t.Context(), &core.IntegrationToken{
		ID:          "identity-audit-token",
		IdentityID:  "triage-bot",
		Integration: "audit-workload-prov",
		Connection:  "workspace",
		Instance:    "team-a",
		AccessToken: "identity-token",
	}); err != nil {
		t.Fatalf("StoreIdentityToken: %v", err)
	}

	broker := invocation.NewBroker(providers, svc.Users, svc.Tokens, invocation.WithAuthorizer(authz))
	guarded := invocation.NewGuarded(broker, broker, "http", auditSink, invocation.WithoutRateLimit())

	srv, err := server.New(server.Config{
		Auth:        &coretesting.StubAuthProvider{N: "test"},
		AuditSink:   auditSink,
		Services:    svc,
		Providers:   providers,
		Invoker:     guarded,
		Authorizer:  authz,
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/audit-workload-prov/ping", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+workloadToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var record map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse audit JSON: %v\nraw: %s", err, auditBuf.String())
	}

	if record["subject_id"] != "identity:triage-bot" {
		t.Fatalf("expected identity subject_id, got %v", record["subject_id"])
	}
	if record["subject_kind"] != "identity" {
		t.Fatalf("expected subject_kind=identity, got %v", record["subject_kind"])
	}
	if record["credential_mode"] != "identity" {
		t.Fatalf("expected credential_mode=identity, got %v", record["credential_mode"])
	}
	if record["credential_subject_id"] != "identity:triage-bot" {
		t.Fatalf("expected credential_subject_id triage-bot, got %v", record["credential_subject_id"])
	}
	if record["credential_connection"] != "workspace" {
		t.Fatalf("expected credential_connection=workspace, got %v", record["credential_connection"])
	}
	if record["credential_instance"] != "team-a" {
		t.Fatalf("expected credential_instance=team-a, got %v", record["credential_instance"])
	}
}

func TestAuditMetadata_OmittedWithoutHTTPContext(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)

	entry := core.AuditEntry{
		RequestID: "req-no-http",
		Source:    "runtime:test",
		Provider:  "alpha",
		Operation: "fetch",
		Allowed:   true,
	}
	auditSink.Log(t.Context(), entry)

	var record map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse audit JSON: %v", err)
	}

	if _, has := record["client_ip"]; has {
		t.Errorf("expected no client_ip for non-HTTP invocation, got %v", record["client_ip"])
	}
	if _, has := record["remote_addr"]; has {
		t.Errorf("expected no remote_addr for non-HTTP invocation, got %v", record["remote_addr"])
	}
	if _, has := record["user_agent"]; has {
		t.Errorf("expected no user_agent for non-HTTP invocation, got %v", record["user_agent"])
	}
}
