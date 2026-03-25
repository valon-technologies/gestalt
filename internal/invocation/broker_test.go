package invocation_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/registry"
	"github.com/valon-technologies/gestalt/internal/testutil"
)

type stubProviderWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubProviderWithOps) ListOperations() []core.Operation {
	return s.ops
}

type stubCatalogProvider struct {
	*stubProviderWithOps
	cat *catalog.Catalog
}

func (s *stubCatalogProvider) Catalog() *catalog.Catalog {
	return s.cat
}

func TestInvoke_Success(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"op":%q,"token":%q}`, op, token),
				}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
			return &core.IntegrationToken{AccessToken: "stored-access-token"}, nil
		},
	}

	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)
	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		UserID:   "u1",
		Source:   principal.SourceSession,
	}

	result, err := b.Invoke(context.Background(), p, "test-int", "", "do_thing", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.Status)
	}
}

func TestInvoke_ProviderNotFound(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	ds := &coretesting.StubDatastore{}
	b := invocation.NewBroker(&reg.Providers, ds)

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		UserID:   "u1",
	}

	_, err := b.Invoke(context.Background(), p, "nonexistent", "", "op", nil)
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got %v", err)
	}
}

func TestInvoke_OperationNotFound(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops:             []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{}
	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		UserID:   "u1",
	}

	_, err := b.Invoke(context.Background(), p, "test-int", "", "nonexistent", nil)
	if !errors.Is(err, invocation.ErrOperationNotFound) {
		t.Fatalf("expected ErrOperationNotFound, got %v", err)
	}
}

func TestInvoke_NilPrincipal(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops:             []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{}
	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)

	_, err := b.Invoke(context.Background(), nil, "test-int", "", "do_thing", nil)
	if !errors.Is(err, invocation.ErrNotAuthenticated) {
		t.Fatalf("expected ErrNotAuthenticated, got %v", err)
	}
}

func TestInvoke_NoStoredToken(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops:             []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
			return nil, core.ErrNotFound
		},
	}
	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		UserID:   "u1",
	}

	_, err := b.Invoke(context.Background(), p, "test-int", "", "do_thing", nil)
	if !errors.Is(err, invocation.ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}

func TestInvoke_UserIDResolution(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "resolved-id", Email: email}, nil
		},
		TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
			return &core.IntegrationToken{AccessToken: "tok"}, nil
		},
	}
	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		Source:   principal.SourceSession,
	}

	_, err := b.Invoke(context.Background(), p, "test-int", "", "do_thing", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.UserID != "resolved-id" {
		t.Fatalf("expected UserID to be resolved, got %q", p.UserID)
	}
}

func TestInvoke_NilTokenResponse(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops:             []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
			return nil, nil
		},
	}
	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		UserID:   "u1",
	}

	_, err := b.Invoke(context.Background(), p, "test-int", "", "do_thing", nil)
	if !errors.Is(err, invocation.ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}

func TestInvoke_MetadataJSONFlowsToContext(t *testing.T) {
	t.Parallel()

	var gotCtx context.Context

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "shopify",
			ExecuteFn: func(ctx context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				gotCtx = ctx
				return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
			},
		},
		ops: []core.Operation{{Name: "list_products"}},
	}

	ds := &coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, _ string) (*core.User, error) {
			return &core.User{ID: "u1"}, nil
		},
		TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
			return &core.IntegrationToken{
				AccessToken:  "tok",
				MetadataJSON: `{"subdomain":"cool-store","region":"us"}`,
			}, nil
		},
	}

	reg := testutil.NewProviderRegistry(t, prov)
	b := invocation.NewBroker(reg, ds)

	p := &principal.Principal{UserID: "u1"}
	_, err := b.Invoke(context.Background(), p, "shopify", "", "list_products", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	cp := core.ConnectionParams(gotCtx)
	if cp == nil {
		t.Fatal("expected connection params in context")
	}
	if cp["subdomain"] != "cool-store" {
		t.Errorf("subdomain = %q, want cool-store", cp["subdomain"])
	}
	if cp["region"] != "us" {
		t.Errorf("region = %q, want us", cp["region"])
	}
}

func TestInvoke_AttachesEgressSubjectFromPrincipal(t *testing.T) {
	t.Parallel()

	var gotSubject egress.Subject

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "test-int",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(ctx context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				gotSubject, _ = egress.SubjectFromContext(ctx)
				return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), &coretesting.StubDatastore{})
	p := &principal.Principal{UserID: "u1"}

	if _, err := b.Invoke(context.Background(), p, "test-int", "", "do_thing", nil); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if gotSubject != (egress.Subject{Kind: egress.SubjectUser, ID: "u1"}) {
		t.Fatalf("subject = %+v, want user u1", gotSubject)
	}
}

func TestInvoke_PreservesExplicitEgressSubject(t *testing.T) {
	t.Parallel()

	var gotSubject egress.Subject

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "test-int",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(ctx context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				gotSubject, _ = egress.SubjectFromContext(ctx)
				return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), &coretesting.StubDatastore{})
	ctx := egress.WithSubject(context.Background(), egress.Subject{Kind: egress.SubjectAgent, ID: "agent-1"})
	p := &principal.Principal{UserID: "u1"}

	if _, err := b.Invoke(ctx, p, "test-int", "", "do_thing", nil); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if gotSubject != (egress.Subject{Kind: egress.SubjectAgent, ID: "agent-1"}) {
		t.Fatalf("subject = %+v, want agent agent-1", gotSubject)
	}
}

func TestInvoke_AttachesIdentitySubjectFromPrincipalEmail(t *testing.T) {
	t.Parallel()

	var gotSubject egress.Subject

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "test-int",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(ctx context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				gotSubject, _ = egress.SubjectFromContext(ctx)
				return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), &coretesting.StubDatastore{})
	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "identity@example.invalid"},
		UserID:   principal.IdentityPrincipal,
	}

	if _, err := b.Invoke(context.Background(), p, "test-int", "", "do_thing", nil); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if gotSubject != (egress.Subject{Kind: egress.SubjectIdentity, ID: "identity@example.invalid"}) {
		t.Fatalf("subject = %+v, want identity email subject", gotSubject)
	}
}

func TestInvoke_AttachesIdentitySubjectFromSentinelPrincipal(t *testing.T) {
	t.Parallel()

	var gotSubject egress.Subject

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "test-int",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(ctx context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				gotSubject, _ = egress.SubjectFromContext(ctx)
				return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), &coretesting.StubDatastore{})
	p := &principal.Principal{UserID: principal.IdentityPrincipal}

	if _, err := b.Invoke(context.Background(), p, "test-int", "", "do_thing", nil); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if gotSubject != (egress.Subject{Kind: egress.SubjectIdentity, ID: principal.IdentityPrincipal}) {
		t.Fatalf("subject = %+v, want identity sentinel subject", gotSubject)
	}
}

func TestBroker_ListCapabilities_IncludesMCPOnlyCatalogOperations(t *testing.T) {
	t.Parallel()

	prov := &stubCatalogProvider{
		stubProviderWithOps: &stubProviderWithOps{
			StubIntegration: coretesting.StubIntegration{N: "clickhouse"},
			ops: []core.Operation{
				{Name: "list_databases", Method: http.MethodGet, Description: "List databases"},
			},
		},
		cat: &catalog.Catalog{
			Name: "clickhouse",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "run_query",
					Title:       "Run Query",
					Description: "Execute a SQL query",
					Transport:   catalog.TransportMCPPassthrough,
					InputSchema: []byte(`{"type":"object","properties":{"sql":{"type":"string"}}}`),
				},
				{
					ID:          "list_databases",
					Method:      http.MethodGet,
					Path:        "/databases",
					Description: "List databases",
				},
			},
		},
	}

	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), &coretesting.StubDatastore{})
	caps := b.ListCapabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}

	byOp := make(map[string]core.Capability, len(caps))
	for _, cap := range caps {
		byOp[cap.Operation] = cap
	}

	runQuery, ok := byOp["run_query"]
	if !ok {
		t.Fatalf("expected run_query capability in %+v", caps)
	}
	if runQuery.Transport != catalog.TransportMCPPassthrough {
		t.Fatalf("run_query transport = %q, want %q", runQuery.Transport, catalog.TransportMCPPassthrough)
	}
	if runQuery.Method != "" {
		t.Fatalf("run_query method = %q, want empty", runQuery.Method)
	}
	if len(runQuery.InputSchema) == 0 {
		t.Fatal("expected run_query input schema to be present")
	}

	listDatabases, ok := byOp["list_databases"]
	if !ok {
		t.Fatalf("expected list_databases capability in %+v", caps)
	}
	if listDatabases.Transport != catalog.TransportHTTP {
		t.Fatalf("list_databases transport = %q, want %q", listDatabases.Transport, catalog.TransportHTTP)
	}
	if listDatabases.Method != http.MethodGet {
		t.Fatalf("list_databases method = %q, want %q", listDatabases.Method, http.MethodGet)
	}
}
