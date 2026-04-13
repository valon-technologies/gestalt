package server_test

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestAuditMetadata_WorkloadSubjectAndCredentialPath(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)

	workloadToken, _, err := principal.GenerateToken(principal.TokenTypeWorkload)
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
		Workloads: map[string]config.WorkloadDef{
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
	if err := svc.Tokens.StoreToken(t.Context(), &core.IntegrationToken{
		ID:          "identity-audit-token",
		UserID:      principal.IdentityPrincipal,
		Integration: "audit-workload-prov",
		Connection:  "workspace",
		Instance:    "team-a",
		AccessToken: "identity-token",
	}); err != nil {
		t.Fatalf("StoreToken: %v", err)
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

	if record["subject_id"] != "workload:triage-bot" {
		t.Fatalf("expected workload subject_id, got %v", record["subject_id"])
	}
	if record["subject_kind"] != "workload" {
		t.Fatalf("expected subject_kind=workload, got %v", record["subject_kind"])
	}
	if record["credential_mode"] != "identity" {
		t.Fatalf("expected credential_mode=identity, got %v", record["credential_mode"])
	}
	if record["credential_subject_id"] != "identity:__identity__" {
		t.Fatalf("expected credential_subject_id identity principal, got %v", record["credential_subject_id"])
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
