package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestAdminManagedSecretRoutes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	cipher := server.NewStaticManagedSecretCipher("projects/test/locations/us-east1/keyRings/gestalt-runtime/cryptoKeys/runtime-secrets", "1", func(_ context.Context, name string, plaintext []byte) ([]byte, error) {
		return []byte(fmt.Sprintf("cipher(%s:%s)", name, plaintext)), nil
	})
	ts := managedSecretTestServer(t, cipher, now)
	testutil.CloseOnCleanup(t, ts)

	t.Run("create rotate list describe retire and audit", func(t *testing.T) {
		t.Parallel()

		create := postManagedSecret(t, ts.URL, map[string]any{
			"name":        "ai-spend-tracker-claude-limit-key",
			"ownerApp":    "ai-spend-tracker",
			"scope":       "app",
			"description": "Claude spend limit key",
			"reason":      "Enable limit sync",
			"source":      "cli",
			"requestId":   "create-1",
			"value":       "first-value",
		})
		if create.Version.Version != 1 || create.RolloutRequired {
			t.Fatalf("create response = %+v", create)
		}
		if create.GeneratedValue != "" {
			t.Fatalf("generated value unexpectedly returned")
		}

		rotate := postManagedSecret(t, ts.URL, map[string]any{
			"name":      "ai-spend-tracker-claude-limit-key",
			"ownerApp":  "ai-spend-tracker",
			"scope":     "app",
			"reason":    "Quarterly rotation",
			"requestId": "rotate-1",
			"value":     "second-value",
		})
		if rotate.Version.Version != 2 || !rotate.RolloutRequired {
			t.Fatalf("rotate response = %+v", rotate)
		}

		var secrets []server.ManagedSecretSummaryAlias
		getJSON(t, ts.URL+"/admin/api/v1/managed-secrets", &secrets)
		var current *server.ManagedSecretSummaryAlias
		for i := range secrets {
			if secrets[i].Name == "ai-spend-tracker-claude-limit-key" {
				current = &secrets[i]
			}
		}
		if current == nil || current.CurrentVer != 2 {
			t.Fatalf("list secrets = %+v", secrets)
		}

		var detail server.ManagedSecretDetailAlias
		getJSON(t, ts.URL+"/admin/api/v1/managed-secrets/ai-spend-tracker-claude-limit-key", &detail)
		if detail.CurrentVer != 2 || len(detail.Audit) != 2 {
			t.Fatalf("detail = %+v", detail)
		}

		var audit []server.ManagedSecretAuditRowAlias
		getJSON(t, ts.URL+"/admin/api/v1/managed-secrets/ai-spend-tracker-claude-limit-key/audit?limit=1", &audit)
		if len(audit) != 1 || audit[0].Action != "rotate" {
			t.Fatalf("audit = %+v", audit)
		}

		retired := postRetireSecret(t, ts.URL, "ai-spend-tracker-claude-limit-key", "No longer used")
		if retired.RetiredAt == nil || retired.RetiredReason != "No longer used" {
			t.Fatalf("retired = %+v", retired)
		}
	})

	t.Run("generated value is returned exactly once", func(t *testing.T) {
		t.Parallel()
		generate := true
		created := postManagedSecret(t, ts.URL, map[string]any{
			"name":     "roadmap-pipeline-secret",
			"ownerApp": "roadmap",
			"reason":   "New signing key",
			"generate": &generate,
		})
		if created.Version.Version != 1 || created.GeneratedValue == "" {
			t.Fatalf("generated response = %+v", created)
		}
		var detail server.ManagedSecretDetailAlias
		getJSON(t, ts.URL+"/admin/api/v1/managed-secrets/roadmap-pipeline-secret", &detail)
		var auditRows []server.ManagedSecretAuditRowAlias
		getJSON(t, ts.URL+"/admin/api/v1/managed-secrets/roadmap-pipeline-secret/audit?limit=10", &auditRows)
		if len(auditRows) == 0 || auditRows[0].Action != "create" {
			t.Fatalf("generated audit = %+v", auditRows)
		}
		if strings.Contains(fmt.Sprintf("%+v", detail), created.GeneratedValue) {
			t.Fatalf("generated value leaked in detail: %+v", detail)
		}
	})

	t.Run("preflight detects missing and unreferenced secrets", func(t *testing.T) {
		t.Parallel()
		body := []server.ManagedSecretReferenceAlias{
			{Provider: "secrets", Name: "missing-secret", App: "ai-spend-tracker", Field: "missingKey"},
		}
		var out server.ManagedSecretPreflightResponseAlias
		postJSON(t, ts.URL+"/admin/api/v1/managed-secrets/preflight", body, &out)
		if out.OK || len(out.Missing) != 1 || out.Missing[0].Name != "missing-secret" {
			t.Fatalf("preflight = %+v", out)
		}
	})

	t.Run("rejects invalid name and missing reason", func(t *testing.T) {
		t.Parallel()
		status, body := postRaw(t, ts.URL+"/admin/api/v1/managed-secrets", map[string]any{
			"name":  "Bad_Name",
			"value": "x",
		})
		if status != http.StatusBadRequest {
			t.Fatalf("invalid name status = %d body %s", status, body)
		}
		status, body = postRaw(t, ts.URL+"/admin/api/v1/managed-secrets", map[string]any{
			"name":  "good-name",
			"value": "x",
		})
		if status != http.StatusBadRequest || !strings.Contains(body, "reason") {
			t.Fatalf("missing reason status = %d body %s", status, body)
		}
	})
}

func managedSecretTestServer(t *testing.T, cipher server.ManagedSecretCipher, now time.Time) *httptest.Server {
	t.Helper()
	svc := testutil.NewStubServices(t)
	user := seedUserRecord(t, svc, "managed-secret-user", "managed-secret-user@example.test", time.Now())
	authz := &serverTestAuthorizationProvider{
		resourceTypes: []*proto.AuthorizationModelResourceType{{
			Name:    "gestalt",
			Actions: []*proto.ModelAction{{Name: "admin", Relations: []string{"admin"}}},
		}},
		relationships: subjectSetGrant(principal.UserSubjectID(user.ID), "admin", "gestalt", "gestalt"),
	}
	return newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = coretesting.NamedIntrospectIdentityStub("test", func(_ context.Context, token string) (*core.UserIdentity, error) {
			if token != "session-token" {
				return nil, core.ErrNotFound
			}
			return &core.UserIdentity{Email: user.Email}, nil
		})
		cfg.Services = svc
		cfg.Authorization = authz
		cfg.Admin = server.AdminRouteConfig{AuthorizationPolicy: "gestalt", AuthorizationAction: "admin"}
		cfg.ManagedSecretCipher = cipher
		cfg.Now = func() time.Time { return now }
	})
}

func managedSecretHTTPClient() *http.Client {
	return &http.Client{Transport: managedSecretTransport{}}
}

type managedSecretTransport struct{}

func (managedSecretTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer session-token")
	return http.DefaultTransport.RoundTrip(req)
}

func getJSON(t *testing.T, url string, target any) {
	t.Helper()
	resp, err := managedSecretHTTPClient().Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("GET %s decode: %v", url, err)
	}
}

func postJSON(t *testing.T, url string, body any, target any) {
	t.Helper()
	status, raw := postRaw(t, url, body)
	if status != http.StatusOK {
		t.Fatalf("POST %s status = %d body %s", url, status, raw)
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		t.Fatalf("POST %s decode: %v", url, err)
	}
}

func postManagedSecret(t *testing.T, baseURL string, body map[string]any) server.ManagedSecretWriteResponseAlias {
	t.Helper()
	status, raw := postRaw(t, baseURL+"/admin/api/v1/managed-secrets", body)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d body %s", status, raw)
	}
	var out server.ManagedSecretWriteResponseAlias
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("create decode: %v", err)
	}
	return out
}

func postRetireSecret(t *testing.T, baseURL, name, reason string) server.ManagedSecretSummaryAlias {
	t.Helper()
	status, raw := postRaw(t, baseURL+"/admin/api/v1/managed-secrets/"+name+"/retire", map[string]any{"reason": reason})
	if status != http.StatusOK {
		t.Fatalf("retire status = %d body %s", status, raw)
	}
	var out server.ManagedSecretSummaryAlias
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("retire decode: %v", err)
	}
	return out
}

func postRaw(t *testing.T, url string, body any) (int, string) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := managedSecretHTTPClient().Post(url, "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, buf.String()
}
