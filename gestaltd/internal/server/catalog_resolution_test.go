package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
)

type stubProvider struct {
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

type stubDynamicSessionProvider struct {
	stubCatalogProvider
	sessionCatFn func(context.Context, string) (*catalog.Catalog, error)
}

func (s *stubDynamicSessionProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if s.sessionCatFn == nil {
		return nil, nil
	}
	return s.sessionCatFn(ctx, token)
}

type stubTokenResolver struct {
	token string
	err   error
}

func (s *stubTokenResolver) ResolveToken(ctx context.Context, _ *principal.Principal, _ string, _ string, _ string) (context.Context, string, error) {
	return ctx, s.token, s.err
}

type stubConnectionTokenResolver struct {
	tokens map[string]string
	errs   map[string]error
}

func (s *stubConnectionTokenResolver) ResolveToken(ctx context.Context, _ *principal.Principal, _ string, connection, _ string) (context.Context, string, error) {
	if s != nil && s.errs != nil {
		if err, ok := s.errs[connection]; ok {
			return ctx, "", err
		}
	}
	if s != nil && s.tokens != nil {
		if token, ok := s.tokens[connection]; ok {
			return ctx, token, nil
		}
	}
	return ctx, "", fmt.Errorf("unexpected connection %q", connection)
}

type stubBoundTokenResolver struct {
	token          string
	lastConnection string
	lastInstance   string
}

func (s *stubBoundTokenResolver) ResolveToken(ctx context.Context, _ *principal.Principal, _ string, connection, instance string) (context.Context, string, error) {
	s.lastConnection = connection
	s.lastInstance = instance
	return ctx, s.token, nil
}

type recordingBindingTokenResolver struct {
	tokenCalls int
}

func (s *recordingBindingTokenResolver) ResolveToken(ctx context.Context, _ *principal.Principal, _ string, _ string, _ string) (context.Context, string, error) {
	s.tokenCalls++
	return ctx, "user-token", nil
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

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "widget-api", nil, nil, "", "")
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

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "gadget-svc", nil, nil, "", "")
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

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "combo-api", resolver, p, "default", "")
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

func TestResolveCatalog_ModeNonePrincipalUsesRequestedSelectors(t *testing.T) {
	t.Parallel()

	prov := &stubDynamicSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "bound-none-api",
				connMode: core.ConnectionModeNone,
			},
		},
		sessionCatFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != "bound-token" {
				t.Fatalf("token = %q, want %q", token, "bound-token")
			}
			return &catalog.Catalog{
				Name: "bound-none-api",
				Operations: []catalog.CatalogOperation{
					{ID: "session_only", Method: http.MethodGet},
				},
			}, nil
		},
	}

	resolver := &stubBoundTokenResolver{
		token: "bound-token",
	}

	cat, err := invocation.ResolveCatalog(
		context.Background(),
		prov,
		"bound-none-api",
		resolver,
		&principal.Principal{Kind: principal.KindWorkload, SubjectID: principal.WorkloadSubjectID("triage-bot")},
		"workspace",
		"team-a",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolver.lastConnection != "workspace" {
		t.Fatalf("resolver connection = %q, want workspace", resolver.lastConnection)
	}
	if resolver.lastInstance != "team-a" {
		t.Fatalf("resolver instance = %q, want team-a", resolver.lastInstance)
	}
	if len(cat.Operations) != 1 || cat.Operations[0].ID != "session_only" {
		t.Fatalf("catalog operations = %+v, want session_only", cat.Operations)
	}
}

func TestResolveCatalog_CredentialModeNoneOverrideSkipsSessionTokenResolution(t *testing.T) {
	t.Parallel()

	prov := &stubDynamicSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "override-none-api",
				connMode: core.ConnectionModeUser,
			},
		},
		sessionCatFn: func(ctx context.Context, token string) (*catalog.Catalog, error) {
			if token != "" {
				t.Fatalf("token = %q, want empty token for credentialMode none override", token)
			}
			if got := invocation.CredentialContextFromContext(ctx).Mode; got != core.ConnectionModeNone {
				t.Fatalf("credential mode = %q, want %q", got, core.ConnectionModeNone)
			}
			return &catalog.Catalog{
				Name: "override-none-api",
				Operations: []catalog.CatalogOperation{
					{ID: "session_only", Method: http.MethodGet},
				},
			}, nil
		},
	}
	resolver := &recordingBindingTokenResolver{}

	ctx := invocation.WithCredentialModeOverride(context.Background(), core.ConnectionModeNone)
	cat, err := invocation.ResolveCatalog(ctx, prov, "override-none-api", resolver, &principal.Principal{UserID: "u1"}, "default", "workspace-a")
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	if resolver.tokenCalls != 0 {
		t.Fatalf("token resolver calls = %d, want 0", resolver.tokenCalls)
	}
	if len(cat.Operations) != 1 || cat.Operations[0].ID != "session_only" {
		t.Fatalf("catalog operations = %+v, want session_only", cat.Operations)
	}
}

func TestResolveCatalogAndOperationMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	prov := &stubDynamicSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "metric-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "metric-api",
				Operations: []catalog.CatalogOperation{
					{ID: "static_op", Method: http.MethodGet},
				},
			},
		},
		sessionCatFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != "bound-token" {
				t.Fatalf("token = %q, want %q", token, "bound-token")
			}
			return &catalog.Catalog{
				Name: "metric-api",
				Operations: []catalog.CatalogOperation{
					{ID: "session_op", Method: http.MethodPost},
				},
			}, nil
		},
	}
	services := coretesting.NewStubServices(t)
	if err := services.ExternalCredentials.PutCredential(ctx, &core.ExternalCredential{
		SubjectID:   principal.UserSubjectID("metrics-user"),
		Integration: "metric-api",
		Connection:  "default",
		Instance:    "default",
		AccessToken: "bound-token",
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}
	resolver := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), services.Users, services.ExternalCredentials)
	p := &principal.Principal{
		Kind:      principal.KindUser,
		UserID:    "metrics-user",
		SubjectID: principal.UserSubjectID("metrics-user"),
	}

	if _, err := invocation.ResolveCatalog(ctx, prov, "metric-api", resolver, p, "default", "default"); err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	if _, _, _, err := invocation.ResolveOperation(ctx, prov, "metric-api", resolver, p, "static_op", nil, ""); err != nil {
		t.Fatalf("ResolveOperation: %v", err)
	}
	if _, _, _, err := invocation.ResolveOperation(ctx, prov, "metric-api", resolver, p, "raw-user-op", nil, ""); !errors.Is(err, invocation.ErrOperationNotFound) {
		t.Fatalf("ResolveOperation(raw-user-op) error = %v, want ErrOperationNotFound", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.catalog.operation.resolve.count", 1, map[string]string{
		"gestalt.provider":  "metric-api",
		"gestalt.operation": "static_op",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.catalog.operation.resolve.count", 1, map[string]string{
		"gestalt.provider":  "metric-api",
		"gestalt.operation": "unknown",
	})
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.catalog.operation.resolve.count", map[string]string{
		"gestalt.provider":  "metric-api",
		"gestalt.operation": "raw-user-op",
	})
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
						Transport:    catalog.TransportREST,
						Description:  "static version",
						AllowedRoles: []string{"admin"},
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
				},
			},
		},
	}

	resolver := &stubTokenResolver{token: "tok_456"}
	p := &principal.Principal{UserID: "u1"}

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "clash-api", resolver, p, "default", "")
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
	if got := cat.Operations[0].AllowedRoles; len(got) != 1 || got[0] != "viewer" {
		t.Fatalf("expected session allowedRoles to win, got %#v", got)
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
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID("u1"), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample-api": {AuthorizationPolicy: "sample_policy"},
	})

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	p := &principal.Principal{
		Kind:      principal.KindUser,
		UserID:    "u1",
		SubjectID: principal.UserSubjectID("u1"),
	}
	cat, err := invocation.ResolveCatalog(context.Background(), prov, "sample-api", &stubTokenResolver{token: "tok_456"}, p, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	filtered := invocation.FilterCatalogForPrincipal(context.Background(), cat, "sample-api", p, authz)
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
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "allow",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID("admin-user"), Role: "admin"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample-api": {AuthorizationPolicy: "sample_policy"},
	})

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	p := &principal.Principal{
		Kind:      principal.KindUser,
		UserID:    "viewer-user",
		SubjectID: principal.UserSubjectID("viewer-user"),
	}
	filtered := invocation.FilterCatalogForPrincipal(context.Background(), prov.Catalog(), "sample-api", p, authz)
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
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "allow",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID("admin-user"), Role: "admin"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample-api": {AuthorizationPolicy: "sample_policy"},
	})

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	p := &principal.Principal{
		Kind:      principal.KindUser,
		UserID:    "viewer-user",
		SubjectID: principal.UserSubjectID("viewer-user"),
	}
	filtered := invocation.FilterCatalogForPrincipal(context.Background(), prov.Catalog(), "sample-api", p, authz)
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

	authz, err := authorization.New(config.AuthorizationConfig{}, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	p := &principal.Principal{
		Kind:      principal.KindUser,
		UserID:    "viewer-user",
		SubjectID: principal.UserSubjectID("viewer-user"),
	}
	filtered := invocation.FilterCatalogForPrincipal(context.Background(), prov.Catalog(), "sample-api", p, authz)
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
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "clash-api",
				Operations: []catalog.CatalogOperation{
					{ID: "shared_op", Method: http.MethodGet, Transport: catalog.TransportREST, Description: "static version", AllowedRoles: []string{"viewer"}},
					{ID: "static_only", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "clash-api",
			Operations: []catalog.CatalogOperation{
				{ID: "shared_op", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough, Description: "session version", AllowedRoles: []string{"viewer"}},
				{ID: "session_only", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough, AllowedRoles: []string{"viewer"}},
			},
		},
	}

	authz, err := authorization.New(config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot-token",
			},
		},
		Policies: map[string]config.SubjectPolicyDef{
			"clash_policy": {
				Members: []config.SubjectPolicyMemberDef{{
					SubjectID: principal.WorkloadSubjectID("triage-bot"),
					Role:      "viewer",
				}},
			},
		},
	}, map[string]*config.ProviderEntry{
		"clash-api": {AuthorizationPolicy: "clash_policy"},
	})

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	p := &principal.Principal{
		Kind:      principal.KindWorkload,
		SubjectID: principal.WorkloadSubjectID("triage-bot"),
	}
	cat, err := invocation.ResolveCatalog(context.Background(), prov, "clash-api", &stubTokenResolver{token: "tok_456"}, p, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	filtered := invocation.FilterCatalogForPrincipal(context.Background(), cat, "clash-api", p, authz)
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

	cat, metadata, err := invocation.ResolveCatalogWithMetadata(context.Background(), prov, "auth-api", resolver, p, "default", "")
	if err != nil {
		t.Fatalf("expected no error on token failure, got: %v", err)
	}
	if !metadata.SessionAttempted {
		t.Fatal("expected session resolution attempt to be reported")
	}
	if !metadata.SessionFailed {
		t.Fatal("expected session resolution failure to be reported")
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation (static only), got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "static_op" {
		t.Fatalf("expected static_op, got %q", cat.Operations[0].ID)
	}
}

func TestResolveCatalogStrict_SessionCatalogUnavailableReturnsTypedError(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "strict-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "strict-api",
				Operations: []catalog.CatalogOperation{
					{ID: "static_op", Method: http.MethodGet},
				},
			},
		},
		sessionErr: fmt.Errorf("upstream catalog failed"),
	}

	resolver := &stubTokenResolver{token: "tok_789"}
	p := &principal.Principal{UserID: "u1"}

	cat, metadata, err := invocation.ResolveCatalogStrictWithMetadata(context.Background(), prov, "strict-api", resolver, p, "default", "")
	if err == nil {
		t.Fatalf("expected strict resolution error, got catalog %+v", cat)
	}
	if !errors.Is(err, core.ErrSessionCatalogUnavailable) {
		t.Fatalf("expected ErrSessionCatalogUnavailable, got %v", err)
	}
	if got, want := err.Error(), "upstream catalog failed"; got != want {
		t.Fatalf("error text = %q, want %q", got, want)
	}
	if !metadata.SessionAttempted {
		t.Fatal("expected session resolution attempt to be reported")
	}
	if !metadata.SessionFailed {
		t.Fatal("expected session resolution failure to be reported")
	}
}

func TestResolveCatalogForTargetsWithMetadata_PrefersLaterSuccessfulTarget(t *testing.T) {
	t.Parallel()

	prov := &stubDynamicSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "multi-target-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "multi-target-api",
				Operations: []catalog.CatalogOperation{
					{ID: "static_op", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			},
		},
		sessionCatFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "catalog-token":
				return nil, fmt.Errorf("catalog unavailable")
			case "default-token":
				return &catalog.Catalog{
					Name: "multi-target-api",
					Operations: []catalog.CatalogOperation{
						{ID: "session_op", Method: http.MethodPost, Transport: catalog.TransportREST},
					},
				}, nil
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	resolver := &stubConnectionTokenResolver{
		tokens: map[string]string{
			"catalog": "catalog-token",
			"default": "default-token",
		},
	}
	p := &principal.Principal{UserID: "u1"}

	cat, metadata, err := invocation.ResolveCatalogForTargetsWithMetadata(
		context.Background(),
		prov,
		"multi-target-api",
		resolver,
		p,
		[]invocation.CatalogResolutionTarget{
			{Connection: "catalog"},
			{Connection: "default"},
		},
		true,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metadata.SessionFailed {
		t.Fatal("expected successful later target to suppress fallback failure metadata")
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("expected merged catalog from later target, got %d operations", len(cat.Operations))
	}
	got := map[string]bool{}
	for _, op := range cat.Operations {
		got[op.ID] = true
	}
	if !got["static_op"] || !got["session_op"] {
		t.Fatalf("expected merged catalog with static_op and session_op, got %#v", got)
	}
}

func TestResolveCatalogForTargetsWithMetadata_FallsBackAfterMixedErrors(t *testing.T) {
	t.Parallel()

	prov := &stubDynamicSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "mixed-errors-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "mixed-errors-api",
				Operations: []catalog.CatalogOperation{
					{ID: "static_op", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			},
		},
		sessionCatFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "session-unavailable-token":
				return nil, fmt.Errorf("catalog upstream unavailable")
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	resolver := &stubConnectionTokenResolver{
		errs: map[string]error{
			"missing": fmt.Errorf("%w: no external credential stored", invocation.ErrNoCredential),
		},
		tokens: map[string]string{
			"default": "session-unavailable-token",
		},
	}
	p := &principal.Principal{UserID: "u1"}

	cat, metadata, err := invocation.ResolveCatalogForTargetsWithMetadata(
		context.Background(),
		prov,
		"mixed-errors-api",
		resolver,
		p,
		[]invocation.CatalogResolutionTarget{
			{Connection: "missing"},
			{Connection: "default"},
		},
		true,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !metadata.SessionAttempted {
		t.Fatal("expected session resolution attempt to be reported")
	}
	if !metadata.SessionFailed {
		t.Fatal("expected fallback metadata to report session failure")
	}
	if len(cat.Operations) != 1 || cat.Operations[0].ID != "static_op" {
		t.Fatalf("catalog operations = %+v, want static fallback", cat.Operations)
	}
}

func TestResolveOperation_DoesNotFallBackOnSessionCatalogError(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "session-op-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "session-op-api",
				Operations: []catalog.CatalogOperation{
					{ID: "static_only", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			},
		},
		sessionErr: fmt.Errorf("session catalog failed"),
	}

	resolver := &stubTokenResolver{token: "tok_123"}
	_, _, _, err := invocation.ResolveOperation(
		context.Background(),
		prov,
		"session-op-api",
		resolver,
		&principal.Principal{UserID: "u1"},
		"static_only",
		[]string{"default"},
		"",
	)
	if err == nil {
		t.Fatal("expected session catalog error")
	}
	if !errors.Is(err, core.ErrSessionCatalogUnavailable) {
		t.Fatalf("expected ErrSessionCatalogUnavailable, got %v", err)
	}
}

func TestResolveOperation_FallsBackWhenSessionCatalogUnsupported(t *testing.T) {
	t.Parallel()

	unsupportedErr := core.WrapSessionCatalogUnsupported(fmt.Errorf("provider %q does not support session catalogs", "session-op-api"))
	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "session-op-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "session-op-api",
				Operations: []catalog.CatalogOperation{
					{ID: "static_only", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			},
		},
		sessionErr: fmt.Errorf("nested session resolution wrapper: %w", unsupportedErr),
	}

	resolver := &stubTokenResolver{token: "tok_123"}
	op, transport, connection, err := invocation.ResolveOperation(
		context.Background(),
		prov,
		"session-op-api",
		resolver,
		&principal.Principal{UserID: "u1"},
		"static_only",
		[]string{"default"},
		"default",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if op.ID != "static_only" {
		t.Fatalf("operation = %q, want %q", op.ID, "static_only")
	}
	if transport != catalog.TransportREST {
		t.Fatalf("transport = %q, want %q", transport, catalog.TransportREST)
	}
	if connection != "" {
		t.Fatalf("connection = %q, want empty", connection)
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

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "noauth-api", nil, &principal.Principal{UserID: "u1"}, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "icon-api", nil, nil, "", "")
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

	_, err := invocation.ResolveCatalog(context.Background(), prov, "clone-api", nil, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if original.Operations[0].InputSchema != nil {
		t.Fatal("CompileSchemas mutated the provider's original catalog")
	}
}
