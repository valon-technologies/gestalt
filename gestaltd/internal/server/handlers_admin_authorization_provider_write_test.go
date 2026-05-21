package server_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestAdminAPI_DeletePluginMemberRemovesPersistedDynamicFragmentAcrossReload(t *testing.T) {
	t.Parallel()

	subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken subject: %v", err)
	}
	subjectID := "service_account:authz-dynamic-candidate"
	svc := testutil.NewStubServices(t)
	seedSubjectAPIToken(t, svc, subjectTokenHash, subjectID, "authz dynamic candidate")

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{StubIntegration: coretesting.StubIntegration{
			N:        "sample_plugin",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
			},
		}},
		catalog: &catalog.Catalog{
			Name:       "sample_plugin",
			Operations: []catalog.CatalogOperation{{ID: "run", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}}},
		},
	}
	pluginDefs := map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy"},
	}
	baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {Default: "deny"},
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", "admin"),
				},
			},
		},
	}, pluginDefs)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz, err := authorization.NewProviderBacked(baseAuthz, provider, authorization.WithDynamicFragmentSource(svc.AuthzFragments))
	if err != nil {
		t.Fatalf("NewProviderBacked: %v", err)
	}
	if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
		t.Fatalf("ReloadAuthorizationState: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "admin-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "static-admin@example.test"}, nil
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.PluginDefs = pluginDefs
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{"admin"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	assertSubjectStatus := func(label string, want int) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample_plugin/run", nil)
		req.Header.Set("Authorization", "Bearer "+subjectToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s request: %v", label, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != want {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s status = %d, want %d: %s", label, resp.StatusCode, want, body)
		}
	}
	assertSubjectStatus("before grant", http.StatusForbidden)

	body := bytes.NewBufferString(fmt.Sprintf(`{"subjectId":%q,"role":"viewer"}`, subjectID))
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT dynamic member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put dynamic member status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	assertSubjectStatus("after grant", http.StatusOK)

	fragment, err := svc.AuthzFragments.GetFragmentByOwner(context.Background(), coredata.AuthorizationPluginFragmentOwner("sample_plugin"))
	if err != nil {
		t.Fatalf("GetFragmentByOwner after grant: %v", err)
	}
	if len(fragment.Relationships) != 1 || fragment.Relationships[0].Subject.ID != subjectID {
		t.Fatalf("fragment after grant = %#v, want one dynamic candidate relationship", fragment.Relationships)
	}

	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members/"+url.PathEscape(subjectID), nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE dynamic member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete dynamic member status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	assertSubjectStatus("after delete", http.StatusForbidden)
	if _, err := svc.AuthzFragments.GetFragmentByOwner(context.Background(), coredata.AuthorizationPluginFragmentOwner("sample_plugin")); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetFragmentByOwner after delete err = %v, want ErrNotFound", err)
	}

	if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
		t.Fatalf("ReloadAuthorizationState after delete: %v", err)
	}
	assertSubjectStatus("after reload", http.StatusForbidden)
	if _, err := svc.AuthzFragments.GetFragmentByOwner(context.Background(), coredata.AuthorizationPluginFragmentOwner("sample_plugin")); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetFragmentByOwner after reload err = %v, want ErrNotFound", err)
	}
}
