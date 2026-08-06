package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestListIntegrationsRequiresCatalogRole(t *testing.T) {
	t.Parallel()

	const sessionToken = "session-token"
	sessionAuth := coretesting.NamedIntrospectIdentityStub("test", func(_ context.Context, token string) (*core.UserIdentity, error) {
		if token != sessionToken {
			return nil, core.ErrNotFound
		}
		return &core.UserIdentity{Email: "user@example.test"}, nil
	})

	svc := testutil.NewStubServices(t)
	user := seedUserRecord(t, svc, "catalog-user", "user@example.test", time.Now())
	subjectID := principal.UserSubjectID(user.ID)

	grantedProvider := &coretesting.StubIntegration{
		N:          "granted-app",
		ConnMode:   core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{ID: "ping", Method: http.MethodGet}}},
	}
	openProvider := &coretesting.StubIntegration{
		N:          "open-app",
		ConnMode:   core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{ID: "ping", Method: http.MethodGet}}},
	}
	lockedProvider := &coretesting.StubIntegration{
		N:          "locked-app",
		ConnMode:   core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{ID: "ping", Method: http.MethodGet}}},
	}

	authz := &serverTestAuthorizationProvider{
		resourceTypes: []*proto.AuthorizationModelResourceType{
			{Name: "app"},
			{Name: "openPolicy", DefaultRole: "viewer"},
		},
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "viewer", "app", "granted-app"),
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = sessionAuth
		cfg.Services = svc
		cfg.Authorization = authz
		cfg.ProviderKinds = map[string]invocation.ProviderKind{
			"granted-app": invocation.ProviderKindApp,
			"open-app":    invocation.ProviderKindApp,
			"locked-app":  invocation.ProviderKindApp,
		}
		cfg.Providers = testutil.NewProviderRegistry(t, grantedProvider, openProvider, lockedProvider)
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"granted-app": {},
			"open-app": {
				AuthorizationPolicy: "openPolicy",
			},
			"locked-app": {},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session_token", Value: sessionToken})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/apps: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}

	var listed []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	names := make(map[string]struct{}, len(listed))
	for _, item := range listed {
		names[item.Name] = struct{}{}
	}
	if _, ok := names["granted-app"]; !ok {
		t.Fatalf("listed apps = %v, want granted-app visible with viewer grant", names)
	}
	if _, ok := names["open-app"]; !ok {
		t.Fatalf("listed apps = %v, want open-app visible via defaultRole viewer", names)
	}
	if _, ok := names["locked-app"]; ok {
		t.Fatalf("listed apps = %v, want locked-app hidden without grant", names)
	}
}

func TestListIntegrationsHidesAppsWithOnlyConnectionDefinitions(t *testing.T) {
	t.Parallel()

	const sessionToken = "session-token"
	sessionAuth := coretesting.NamedIntrospectIdentityStub("test", func(_ context.Context, token string) (*core.UserIdentity, error) {
		if token != sessionToken {
			return nil, core.ErrNotFound
		}
		return &core.UserIdentity{Email: "user@example.test"}, nil
	})

	svc := testutil.NewStubServices(t)
	seedUserRecord(t, svc, "catalog-user", "user@example.test", time.Now())

	provider := &coretesting.StubIntegration{
		N:          "oauth-only",
		ConnMode:   core.ConnectionModeSubject,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{ID: "ping", Method: http.MethodGet}}},
	}

	authz := &serverTestAuthorizationProvider{
		resourceTypes: []*proto.AuthorizationModelResourceType{{Name: "app"}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = sessionAuth
		cfg.Services = svc
		cfg.Authorization = authz
		cfg.ProviderKinds = map[string]invocation.ProviderKind{
			"oauth-only": invocation.ProviderKindApp,
		}
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.AppDefs = testPluginDefsForConnections("oauth-only", testDefaultConnection)
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session_token", Value: sessionToken})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/apps: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}

	var listed []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, item := range listed {
		if item.Name == "oauth-only" {
			t.Fatalf("listed apps = %v, want oauth-only hidden without viewer/editor/admin grant", listed)
		}
	}
}
