package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type stubProvider struct {
	core.NoOAuth
	name        string
	displayName string
	description string
	connMode    core.ConnectionMode
	ops         []core.Operation
}

func (s *stubProvider) Name() string                        { return s.name }
func (s *stubProvider) DisplayName() string                 { return s.displayName }
func (s *stubProvider) Description() string                 { return s.description }
func (s *stubProvider) ConnectionMode() core.ConnectionMode { return s.connMode }
func (s *stubProvider) AuthTypes() []string                 { return nil }
func (s *stubProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (s *stubProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (s *stubProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (s *stubProvider) ConnectionForOperation(string) string        { return "" }
func (s *stubProvider) Catalog() *catalog.Catalog                   { return nil }
func (s *stubProvider) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, nil
}

type stubCatalogProvider struct {
	stubProvider
	cat *catalog.Catalog
}

func (s *stubCatalogProvider) Catalog() *catalog.Catalog { return s.cat }

type stubSessionProvider struct {
	stubCatalogProvider
	sessionCat *catalog.Catalog
	sessionErr error
}

func (s *stubSessionProvider) CatalogForRequest(_ context.Context, _ string) (*catalog.Catalog, error) {
	return s.sessionCat, s.sessionErr
}

type stubTokenResolver struct {
	token string
	err   error
}

func (s *stubTokenResolver) ResolveToken(ctx context.Context, _ *principal.Principal, _ string, _ string, _ string) (context.Context, string, error) {
	return ctx, s.token, s.err
}

func resolveCatalogForTest(ctx context.Context, prov core.Provider, provName string, resolver invocation.TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, error) {
	cat, _, err := invocation.ResolveCatalogWithMetadata(ctx, prov, provName, resolver, p, defaultConnection, instance, false)
	return cat, err
}

func TestResolveCatalog_StaticCatalog(t *testing.T) {
	t.Parallel()

	prov := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:        "widget-api",
			displayName: "Widget API",
			description: "Manages widgets",
			connMode:    core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{
			Name:        "widget-api",
			DisplayName: "Widget API",
			Operations: []catalog.CatalogOperation{
				{
					ID:     "list_widgets",
					Method: http.MethodGet,
					Path:   "/widgets",
					Parameters: []catalog.CatalogParameter{
						{Name: "page", Type: "integer", Location: "query", Required: false},
						{Name: "limit", Type: "integer", Location: "query", Required: true},
					},
				},
			},
		},
	}

	cat, err := resolveCatalogForTest(context.Background(), prov, "widget-api", nil, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(cat.Operations))
	}
	op := cat.Operations[0]
	if op.ID != "list_widgets" {
		t.Fatalf("expected id %q, got %q", "list_widgets", op.ID)
	}
	if op.InputSchema == nil {
		t.Fatal("expected inputSchema to be synthesized")
	}
	var schema map[string]any
	if err := json.Unmarshal(op.InputSchema, &schema); err != nil {
		t.Fatalf("invalid inputSchema JSON: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}
	if _, ok := props["page"]; !ok {
		t.Fatal("expected page in schema properties")
	}
	if _, ok := props["limit"]; !ok {
		t.Fatal("expected limit in schema properties")
	}
}

func TestResolveCatalog_FlatProviderErrors(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:        "gadget-svc",
		displayName: "Gadget Service",
		description: "Gadget operations",
		connMode:    core.ConnectionModeNone,
		ops: []core.Operation{
			{
				Name:        "create_gadget",
				Method:      http.MethodPost,
				Description: "Creates a gadget",
				Parameters: []core.Parameter{
					{Name: "label", Type: "string", Required: true},
					{Name: "count", Type: "integer", Required: false, Default: 1},
				},
			},
			{
				Name:        "get_gadget",
				Method:      http.MethodGet,
				Description: "Gets a gadget",
			},
		},
	}

	cat, err := resolveCatalogForTest(context.Background(), prov, "gadget-svc", nil, nil, "", "")
	if err == nil {
		t.Fatalf("expected error for provider without catalog, got catalog %+v", cat)
	}
	if got, want := err.Error(), `provider "gadget-svc" does not expose a catalog`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestResolveCatalog_SessionAndStaticMerge(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "combo-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "combo-api",
				Operations: []catalog.CatalogOperation{
					{ID: "rest_op", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "combo-api",
			Operations: []catalog.CatalogOperation{
				{ID: "mcp_op", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
			},
		},
	}

	resolver := &stubTokenResolver{token: "tok_123"}
	p := &principal.Principal{UserID: "u1"}

	cat, err := resolveCatalogForTest(context.Background(), prov, "combo-api", resolver, p, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(cat.Operations))
	}

	ids := map[string]bool{}
	for _, op := range cat.Operations {
		ids[op.ID] = true
	}
	if !ids["rest_op"] {
		t.Fatal("expected rest_op in merged catalog")
	}
	if !ids["mcp_op"] {
		t.Fatal("expected mcp_op in merged catalog")
	}
}

func TestResolveCatalog_SameIDCollision_SessionWins(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "clash-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "clash-api",
				Operations: []catalog.CatalogOperation{
					{
						ID:           "shared_op",
						Method:       http.MethodGet,
						Path:         "/widgets/{id}",
						Transport:    catalog.TransportREST,
						Description:  "static version",
						AllowedRoles: []string{"admin"},
						Query:        "query StaticWidget { widget { id } }",
						Parameters: []catalog.CatalogParameter{
							{Name: "widgetId", Type: "string", Location: "path", WireName: "id", Required: true},
							{Name: "pageSize", Type: "integer", Location: "query", WireName: "page[size]"},
						},
					},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "clash-api",
			Operations: []catalog.CatalogOperation{
				{
					ID:           "shared_op",
					Method:       http.MethodPost,
					Transport:    catalog.TransportMCPPassthrough,
					Description:  "session version",
					AllowedRoles: []string{"viewer"},
					Parameters: []catalog.CatalogParameter{
						{Name: "pageSize", Type: "integer"},
					},
				},
			},
		},
	}

	resolver := &stubTokenResolver{token: "tok_456"}
	p := &principal.Principal{UserID: "u1"}

	cat, err := resolveCatalogForTest(context.Background(), prov, "clash-api", resolver, p, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation after collision, got %d", len(cat.Operations))
	}
	if cat.Operations[0].Description != "session version" {
		t.Fatalf("expected session version to win, got %q", cat.Operations[0].Description)
	}
	if cat.Operations[0].Method != http.MethodPost {
		t.Fatalf("expected POST from session, got %q", cat.Operations[0].Method)
	}
	if cat.Operations[0].Path != "/widgets/{id}" {
		t.Fatalf("expected static path to fill sparse session op, got %q", cat.Operations[0].Path)
	}
	if cat.Operations[0].Query != "query StaticWidget { widget { id } }" {
		t.Fatalf("expected static query to fill sparse session op, got %q", cat.Operations[0].Query)
	}
	if got := cat.Operations[0].AllowedRoles; len(got) != 1 || got[0] != "viewer" {
		t.Fatalf("expected session allowedRoles to win, got %#v", got)
	}
	if got := cat.Operations[0].Parameters; len(got) != 2 {
		t.Fatalf("expected merged parameters, got %#v", got)
	} else {
		paramsByName := map[string]catalog.CatalogParameter{}
		for _, param := range got {
			paramsByName[param.Name] = param
		}
		if got := paramsByName["widgetId"]; got.Location != "path" || got.WireName != "id" || !got.Required {
			t.Fatalf("expected static path param to be appended and preserved, got %#v", got)
		}
		if got := paramsByName["pageSize"]; got.Location != "query" || got.WireName != "page[size]" {
			t.Fatalf("expected static query metadata on pageSize, got %#v", got)
		}
	}

	op, transport, connection, err := invocation.ResolveOperation(context.Background(), prov, "clash-api", resolver, p, "shared_op", []string{"default"}, "")
	if err != nil {
		t.Fatalf("ResolveOperation: %v", err)
	}
	if transport != catalog.TransportMCPPassthrough {
		t.Fatalf("transport = %q, want %q", transport, catalog.TransportMCPPassthrough)
	}
	if connection != "default" {
		t.Fatalf("connection = %q, want %q", connection, "default")
	}
	if op.Method != http.MethodPost {
		t.Fatalf("resolved method = %q, want %q", op.Method, http.MethodPost)
	}
	if op.Path != "/widgets/{id}" {
		t.Fatalf("resolved path = %q, want %q", op.Path, "/widgets/{id}")
	}
	if got := op.Parameters; len(got) != 2 {
		t.Fatalf("expected merged resolved parameters, got %#v", got)
	}

	sparseProv := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "sparse-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "sparse-api",
				Operations: []catalog.CatalogOperation{
					{
						ID:        "shared_op",
						Method:    http.MethodPost,
						Transport: catalog.TransportMCPPassthrough,
					},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "sparse-api",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "shared_op",
					Description: "session version",
				},
			},
		},
	}

	sparseCat, err := resolveCatalogForTest(context.Background(), sparseProv, "sparse-api", resolver, p, "default", "")
	if err != nil {
		t.Fatalf("unexpected sparse collision error: %v", err)
	}
	if len(sparseCat.Operations) != 1 {
		t.Fatalf("expected 1 sparse operation after collision, got %d", len(sparseCat.Operations))
	}
	if sparseCat.Operations[0].Transport != catalog.TransportMCPPassthrough {
		t.Fatalf("sparse collision transport = %q, want %q", sparseCat.Operations[0].Transport, catalog.TransportMCPPassthrough)
	}

	sparseOp, sparseTransport, _, err := invocation.ResolveOperation(context.Background(), sparseProv, "sparse-api", resolver, p, "shared_op", []string{"default"}, "")
	if err != nil {
		t.Fatalf("ResolveOperation sparse collision: %v", err)
	}
	if sparseTransport != catalog.TransportMCPPassthrough {
		t.Fatalf("sparse collision resolved transport = %q, want %q", sparseTransport, catalog.TransportMCPPassthrough)
	}
	if sparseOp.Transport != catalog.TransportMCPPassthrough {
		t.Fatalf("sparse collision op transport = %q, want %q", sparseOp.Transport, catalog.TransportMCPPassthrough)
	}
}

func TestFilterCatalogForPrincipal_HumanFilteringUsesResolvedRole(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "sample-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "sample-api",
				Operations: []catalog.CatalogOperation{
					{ID: "public_op", Method: http.MethodGet, Transport: catalog.TransportREST},
					{ID: "viewer_op", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
					{ID: "admin_op", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "sample-api",
			Operations: []catalog.CatalogOperation{
				{ID: "session_viewer", Method: http.MethodPost, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
				{ID: "session_admin", Method: http.MethodPost, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
			},
		},
	}

	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID("u1"), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample-api": {AuthorizationPolicy: "sample_policy"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	p := &principal.Principal{
		Kind:      principal.KindUser,
		UserID:    "u1",
		SubjectID: principal.UserSubjectID("u1"),
	}
	cat, err := resolveCatalogForTest(context.Background(), prov, "sample-api", &stubTokenResolver{token: "tok_456"}, p, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	filtered := invocation.FilterCatalogForPrincipal(cat, "sample-api", p, authz)
	if len(filtered.Operations) != 2 {
		t.Fatalf("expected 2 operations after human filtering, got %d", len(filtered.Operations))
	}
	gotIDs := []string{
		filtered.Operations[0].ID,
		filtered.Operations[1].ID,
	}
	wantIDs := []string{"viewer_op", "session_viewer"}
	if fmt.Sprint(gotIDs) != fmt.Sprint(wantIDs) {
		t.Fatalf("filtered operation IDs = %#v, want %#v", gotIDs, wantIDs)
	}
}

func TestFilterCatalogForPrincipal_HumanDefaultAllowKeepsUnannotatedOperations(t *testing.T) {
	t.Parallel()

	prov := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:     "sample-api",
			connMode: core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{
			Name: "sample-api",
			Operations: []catalog.CatalogOperation{
				{ID: "baseline_op", Method: http.MethodGet, Transport: catalog.TransportREST},
				{ID: "admin_op", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
			},
		},
	}

	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "allow",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID("admin-user"), Role: "admin"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample-api": {AuthorizationPolicy: "sample_policy"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	p := &principal.Principal{
		Kind:      principal.KindUser,
		UserID:    "viewer-user",
		SubjectID: principal.UserSubjectID("viewer-user"),
	}
	filtered := invocation.FilterCatalogForPrincipal(prov.Catalog(), "sample-api", p, authz)
	if len(filtered.Operations) != 1 {
		t.Fatalf("expected 1 operation after default-allow filtering, got %d", len(filtered.Operations))
	}
	if filtered.Operations[0].ID != "baseline_op" {
		t.Fatalf("filtered operation ID = %q, want baseline_op", filtered.Operations[0].ID)
	}
}

func TestFilterCatalogForPrincipal_HumanDefaultAllowTreatsUnmatchedUsersAsViewer(t *testing.T) {
	t.Parallel()

	prov := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:     "sample-api",
			connMode: core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{
			Name: "sample-api",
			Operations: []catalog.CatalogOperation{
				{ID: "baseline_op", Method: http.MethodGet, Transport: catalog.TransportREST},
				{ID: "viewer_op", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
				{ID: "admin_op", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
			},
		},
	}

	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "allow",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID("admin-user"), Role: "admin"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample-api": {AuthorizationPolicy: "sample_policy"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	p := &principal.Principal{
		Kind:      principal.KindUser,
		UserID:    "viewer-user",
		SubjectID: principal.UserSubjectID("viewer-user"),
	}
	filtered := invocation.FilterCatalogForPrincipal(prov.Catalog(), "sample-api", p, authz)
	if len(filtered.Operations) != 2 {
		t.Fatalf("expected 2 operations after default-allow filtering, got %d", len(filtered.Operations))
	}
	gotIDs := []string{filtered.Operations[0].ID, filtered.Operations[1].ID}
	wantIDs := []string{"baseline_op", "viewer_op"}
	if fmt.Sprint(gotIDs) != fmt.Sprint(wantIDs) {
		t.Fatalf("filtered operation IDs = %#v, want %#v", gotIDs, wantIDs)
	}
}

func TestFilterCatalogForPrincipal_HumanUnboundProviderKeepsRoleAnnotatedOperations(t *testing.T) {
	t.Parallel()

	prov := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:     "sample-api",
			connMode: core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{
			Name: "sample-api",
			Operations: []catalog.CatalogOperation{
				{ID: "baseline_op", Method: http.MethodGet, Transport: catalog.TransportREST},
				{ID: "viewer_op", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
			},
		},
	}

	authz, err := authorization.New(config.AuthorizationConfig{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	p := &principal.Principal{
		Kind:      principal.KindUser,
		UserID:    "viewer-user",
		SubjectID: principal.UserSubjectID("viewer-user"),
	}
	filtered := invocation.FilterCatalogForPrincipal(prov.Catalog(), "sample-api", p, authz)
	if len(filtered.Operations) != 2 {
		t.Fatalf("expected 2 operations after filtering unbound provider, got %d", len(filtered.Operations))
	}
	gotIDs := []string{filtered.Operations[0].ID, filtered.Operations[1].ID}
	wantIDs := []string{"baseline_op", "viewer_op"}
	if fmt.Sprint(gotIDs) != fmt.Sprint(wantIDs) {
		t.Fatalf("filtered operation IDs = %#v, want %#v", gotIDs, wantIDs)
	}
}

func TestFilterCatalogForPrincipal_WorkloadFilteringUsesMergedCatalog(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "clash-api",
				connMode: core.ConnectionModeIdentity,
			},
			cat: &catalog.Catalog{
				Name: "clash-api",
				Operations: []catalog.CatalogOperation{
					{ID: "shared_op", Method: http.MethodGet, Transport: catalog.TransportREST, Description: "static version"},
					{ID: "static_only", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "clash-api",
			Operations: []catalog.CatalogOperation{
				{ID: "shared_op", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough, Description: "session version"},
				{ID: "session_only", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
			},
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	authz, err := authorization.New(config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot-token",
				Providers: map[string]config.WorkloadProviderDef{
					"clash-api": {
						Connection: "default",
						Instance:   "default",
						Allow:      []string{"shared_op", "session_only"},
					},
				},
			},
		},
	}, nil, providers, map[string]string{"clash-api": "default"})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	p := &principal.Principal{
		Kind:      principal.KindWorkload,
		SubjectID: principal.WorkloadSubjectID("triage-bot"),
	}
	cat, err := resolveCatalogForTest(context.Background(), prov, "clash-api", &stubTokenResolver{token: "tok_456"}, p, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	filtered := invocation.FilterCatalogForPrincipal(cat, "clash-api", p, authz)
	if len(filtered.Operations) != 2 {
		t.Fatalf("expected 2 operations after workload filtering, got %d", len(filtered.Operations))
	}
	if filtered.Operations[0].ID != "shared_op" {
		t.Fatalf("expected first filtered op shared_op, got %q", filtered.Operations[0].ID)
	}
	if filtered.Operations[0].Description != "session version" {
		t.Fatalf("expected session version to win after filtering, got %q", filtered.Operations[0].Description)
	}
	if filtered.Operations[1].ID != "session_only" {
		t.Fatalf("expected second filtered op session_only, got %q", filtered.Operations[1].ID)
	}
}

func TestResolveCatalog_TokenResolutionFailure_NonFatal(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "auth-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "auth-api",
				Operations: []catalog.CatalogOperation{
					{ID: "static_op", Method: http.MethodGet},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "auth-api",
			Operations: []catalog.CatalogOperation{
				{ID: "session_only", Method: http.MethodPost},
			},
		},
	}

	resolver := &stubTokenResolver{err: fmt.Errorf("token expired")}
	p := &principal.Principal{UserID: "u1"}

	cat, sessionFailed, err := invocation.ResolveCatalogWithMetadata(context.Background(), prov, "auth-api", resolver, p, "default", "", false)
	if err != nil {
		t.Fatalf("expected no error on token failure, got: %v", err)
	}
	if !sessionFailed {
		t.Fatal("expected session resolution failure to be reported")
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation (static only), got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "static_op" {
		t.Fatalf("expected static_op, got %q", cat.Operations[0].ID)
	}
}

func TestResolveCatalog_NilResolver(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "noauth-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "noauth-api",
				Operations: []catalog.CatalogOperation{
					{ID: "the_op", Method: http.MethodPut},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "noauth-api",
			Operations: []catalog.CatalogOperation{
				{ID: "hidden_op", Method: http.MethodPost},
			},
		},
	}

	cat, sessionFailed, err := invocation.ResolveCatalogWithMetadata(context.Background(), prov, "noauth-api", nil, &principal.Principal{UserID: "u1"}, "default", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sessionFailed {
		t.Fatal("expected skipped session lookup not to report failure")
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation (static only), got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "the_op" {
		t.Fatalf("expected the_op, got %q", cat.Operations[0].ID)
	}
}

func TestResolveCatalog_IconOnlyCatalogPreserved(t *testing.T) {
	t.Parallel()

	prov := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:        "icon-api",
			displayName: "Icon API",
			description: "Has icon and operations",
			connMode:    core.ConnectionModeUser,
			ops: []core.Operation{
				{
					Name:        "do_thing",
					Method:      http.MethodPost,
					Description: "Does a thing",
					Parameters: []core.Parameter{
						{Name: "input", Type: "string", Required: true},
					},
				},
			},
		},
		cat: &catalog.Catalog{
			Name:    "icon-api",
			IconSVG: `<svg/>`,
		},
	}

	cat, err := resolveCatalogForTest(context.Background(), prov, "icon-api", nil, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cat.IconSVG != `<svg/>` {
		t.Fatalf("IconSVG = %q, want %q", cat.IconSVG, `<svg/>`)
	}
	if len(cat.Operations) != 0 {
		t.Fatalf("got %d operations, want 0", len(cat.Operations))
	}
}

func TestResolveCatalog_CloneSafety(t *testing.T) {
	t.Parallel()

	original := &catalog.Catalog{
		Name: "clone-api",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "safe_op",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "data", Type: "string", Required: true},
				},
			},
		},
	}

	prov := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:     "clone-api",
			connMode: core.ConnectionModeNone,
		},
		cat: original,
	}

	_, err := resolveCatalogForTest(context.Background(), prov, "clone-api", nil, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if original.Operations[0].InputSchema != nil {
		t.Fatal("CompileSchemas mutated the provider's original catalog")
	}
}
