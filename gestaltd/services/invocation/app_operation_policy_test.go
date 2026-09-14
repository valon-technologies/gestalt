package invocation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestAppOperationPolicySharedStoreCoversInvocationModesAndSessionDiscovery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	providerA := newAppOperationPolicyProvider("workspace")
	providerB := newAppOperationPolicyProvider("workspace")
	p := appOperationPolicyPrincipal()
	authz := appOperationPolicyAuthz(p.SubjectID, []string{"viewer"})
	brokerA := NewBroker(
		testutil.NewProviderRegistry(t, providerA),
		svc.Users,
		svc.ExternalCredentials,
		WithAuthorizationProvider(authz),
		WithProviderKinds(map[string]ProviderKind{"workspace": ProviderKindApp}),
		WithAppOperationPolicies(svc.AppAllowedOperations),
	)
	brokerB := NewBroker(
		testutil.NewProviderRegistry(t, providerB),
		svc.Users,
		svc.ExternalCredentials,
		WithAuthorizationProvider(authz),
		WithProviderKinds(map[string]ProviderKind{"workspace": ProviderKindApp}),
		WithAppOperationPolicies(svc.AppAllowedOperations),
	)

	if err := svc.AppAllowedOperations.Patch(ctx, "workspace", core.AppOperationPolicy{
		"unary.op":    {"viewer"},
		"stream.op":   {"viewer"},
		"maybe.op":    {"viewer"},
		"graphql":     {"viewer"},
		"dynamic.mcp": {"viewer"},
	}); err != nil {
		t.Fatalf("Patch viewer policy: %v", err)
	}

	modes := []struct {
		name string
		op   string
		call func(*testing.T, *Broker, *principal.Principal)
	}{
		{
			name: "unary",
			op:   "unary.op",
			call: func(t *testing.T, broker *Broker, p *principal.Principal) {
				t.Helper()
				result, err := broker.Invoke(ctx, p, "workspace", "", "unary.op", nil)
				if err != nil {
					t.Fatalf("Invoke unary: %v", err)
				}
				requirePolicyResult(t, result, `{"operation":"unary.op"}`)
			},
		},
		{
			name: "stream",
			op:   "stream.op",
			call: func(t *testing.T, broker *Broker, p *principal.Principal) {
				t.Helper()
				reader, err := broker.InvokeStream(ctx, p, "workspace", "", "stream.op", nil)
				if err != nil {
					t.Fatalf("InvokeStream: %v", err)
				}
				requirePolicyStream(t, reader)
			},
		},
		{
			name: "maybe",
			op:   "maybe.op",
			call: func(t *testing.T, broker *Broker, p *principal.Principal) {
				t.Helper()
				outcome, err := broker.InvokeMaybeStream(ctx, p, "workspace", "", "maybe.op", nil)
				if err != nil {
					t.Fatalf("InvokeMaybeStream: %v", err)
				}
				if outcome == nil || outcome.Unary == nil || outcome.IsStream() {
					t.Fatalf("InvokeMaybeStream outcome = %+v, want unary result", outcome)
				}
				requirePolicyResult(t, outcome.Unary, `{"operation":"maybe.op"}`)
			},
		},
		{
			name: "graphql",
			op:   core.GraphQLCapabilityID,
			call: func(t *testing.T, broker *Broker, p *principal.Principal) {
				t.Helper()
				result, err := broker.InvokeGraphQL(ctx, p, "workspace", "", core.GraphQLRequest{Document: "query { viewer { id } }"})
				if err != nil {
					t.Fatalf("InvokeGraphQL: %v", err)
				}
				requirePolicyResult(t, result, `{"data":{}}`)
			},
		},
		{
			name: "dynamic",
			op:   "dynamic.mcp",
			call: func(t *testing.T, broker *Broker, p *principal.Principal) {
				t.Helper()
				result, err := broker.Invoke(ctx, p, "workspace", "", "dynamic.mcp", nil)
				if err != nil {
					t.Fatalf("Invoke dynamic session operation: %v", err)
				}
				requirePolicyResult(t, result, `{"operation":"dynamic.mcp"}`)
			},
		},
	}

	for _, mode := range modes {

		mode.call(t, brokerA, p)
		if mode.op == "dynamic.mcp" {
			requireDynamicPolicyListing(t, ctx, providerA, brokerA, p, true)
		}

		if err := svc.AppAllowedOperations.Patch(ctx, "workspace", core.AppOperationPolicy{mode.op: nil}); err != nil {
			t.Fatalf("Patch deny policy: %v", err)
		}
		providerB.failCall = func(op string) {
			t.Fatalf("denied %s operation reached provider as %q", mode.name, op)
		}
		assertPolicyModeDenied(t, ctx, brokerB, p, mode.name, mode.op)
		if mode.op == "dynamic.mcp" {
			requireDynamicPolicyListing(t, ctx, providerB, brokerB, p, false)
		}

		if err := svc.AppAllowedOperations.Patch(ctx, "workspace", core.AppOperationPolicy{mode.op: {"viewer"}}); err != nil {
			t.Fatalf("Patch re-enable policy: %v", err)
		}
		providerB.failCall = nil
		mode.call(t, brokerB, p)
		if mode.op == "dynamic.mcp" {
			requireDynamicPolicyListing(t, ctx, providerB, brokerB, p, true)
		}
	}

	if op := requireCatalogOperation(t, providerA.Catalog(), "unary.op"); op.Description != "static unary metadata" || len(op.AllowedRoles) != 1 || op.AllowedRoles[0] != "admin" {
		t.Fatalf("provider A static metadata mutated: %+v", op)
	}
	if op := requireCatalogOperation(t, providerB.Catalog(), "unary.op"); op.Description != "static unary metadata" || len(op.AllowedRoles) != 1 || op.AllowedRoles[0] != "admin" {
		t.Fatalf("provider B static metadata mutated: %+v", op)
	}
}

func TestAppOperationPolicySupersedesStaticRolesForSingleAndBatchAccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	provider := newAppOperationPolicyProvider("workspace")
	p := appOperationPolicyPrincipal()
	authz := appOperationPolicyAuthz(p.SubjectID, []string{"viewer"})
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		svc.ExternalCredentials,
		WithAuthorizationProvider(authz),
		WithProviderKinds(map[string]ProviderKind{"workspace": ProviderKindApp}),
		WithAppOperationPolicies(svc.AppAllowedOperations),
	)
	if _, err := broker.InvokeGraphQL(ctx, p, "workspace", "", core.GraphQLRequest{Document: "query { viewer { id } }"}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("raw GraphQL must inherit catalog admin roles without an override, got %v", err)
	}
	if err := svc.AppAllowedOperations.Patch(ctx, "workspace", core.AppOperationPolicy{
		"unary.op": {"viewer"},
	}); err != nil {
		t.Fatalf("Patch viewer policy: %v", err)
	}

	if err := broker.CheckOperationAccess(ctx, p, "workspace", "unary.op"); err != nil {
		t.Fatalf("CheckOperationAccess = %v, want allowed by runtime viewer role", err)
	}
	results, err := broker.CheckOperationAccessMany(ctx, p, []OperationAccessQuery{{
		Provider:     "workspace",
		Operation:    "unary.op",
		AllowedRoles: []string{"admin"},
	}})
	if err != nil {
		t.Fatalf("CheckOperationAccessMany: %v", err)
	}
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("batch access results = %#v, want one allowed result", results)
	}
}

func TestAppOperationPolicyFailureFailsClosedForInvokeAndBatchListing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	provider := newAppOperationPolicyProvider("workspace")
	p := appOperationPolicyPrincipal()
	store := failingAppOperationPolicyStore{err: errors.New("indexeddb unavailable")}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		nil,
		nil,
		WithAppOperationPolicies(store),
	)

	if _, err := broker.Invoke(ctx, p, "workspace", "", "unary.op", nil); !errors.Is(err, ErrAuthorizationUnavailable) {
		t.Fatalf("Invoke with failing policy store = %v, want ErrAuthorizationUnavailable", err)
	}
	if _, err := FilterCatalogForPrincipal(ctx, provider.Catalog(), "workspace", p, broker); !errors.Is(err, ErrAuthorizationUnavailable) {
		t.Fatalf("FilterCatalogForPrincipal with failing policy store = %v, want ErrAuthorizationUnavailable", err)
	}
}

func TestAppOperationPolicyCannotBypassTokenScopeOrAppAccessProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	provider := newAppOperationPolicyProvider("workspace")
	if err := svc.AppAllowedOperations.Patch(ctx, "workspace", core.AppOperationPolicy{
		"unary.op": {"viewer"},
	}); err != nil {
		t.Fatalf("Patch viewer policy: %v", err)
	}
	authz := appOperationPolicyAuthz(principal.UserSubjectID("policy-user"), []string{"viewer"})
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		svc.ExternalCredentials,
		WithAuthorizationProvider(authz),
		WithProviderKinds(map[string]ProviderKind{"workspace": ProviderKindApp}),
		WithAppOperationPolicies(svc.AppAllowedOperations),
		WithAppAccessProfiles(svc.AppAccessProfiles),
	)

	scoped := &principal.Principal{
		SubjectID: principal.UserSubjectID("policy-user"),
		UserID:    "policy-user",
		Kind:      principal.KindUser,
		Scopes:    []string{"workspace:other.op"},
	}
	if _, err := broker.Invoke(ctx, scoped, "workspace", "", "unary.op", nil); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("Invoke with missing token operation scope = %v, want ErrScopeDenied", err)
	}

	unscoped := appOperationPolicyPrincipal()
	if _, err := svc.AppAccessProfiles.SetAppAccessOperations(ctx, unscoped.SubjectID, "workspace", []string{"other.op"}); err != nil {
		t.Fatalf("SetAppAccessOperations: %v", err)
	}
	if _, err := broker.Invoke(ctx, unscoped, "workspace", "", "unary.op", nil); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("Invoke outside app access profile = %v, want ErrAuthorizationDenied", err)
	}
}

func TestAppOperationPolicyRemoteDelegatedProviderKeepsRoleDelegationButDeniesLocally(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	provider := &appOperationPolicyRemoteProvider{appOperationPolicyProvider: newAppOperationPolicyProvider("remote")}
	p := appOperationPolicyPrincipal()
	authz := appOperationPolicyAuthz(p.SubjectID, []string{"viewer"})
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		svc.ExternalCredentials,
		WithAuthorizationProvider(authz),
		WithAppOperationPolicies(svc.AppAllowedOperations),
	)

	if err := svc.AppAllowedOperations.Patch(ctx, "remote", core.AppOperationPolicy{
		"unary.op": {"admin"},
	}); err != nil {
		t.Fatalf("Patch delegated non-empty role policy: %v", err)
	}
	result, err := broker.Invoke(ctx, p, "remote", "", "unary.op", nil)
	if err != nil {
		t.Fatalf("remote delegated non-empty runtime roles should not be locally role-evaluated: %v", err)
	}
	requirePolicyResult(t, result, `{"operation":"unary.op"}`)

	if err := svc.AppAllowedOperations.Patch(ctx, "remote", core.AppOperationPolicy{
		"unary.op": nil,
	}); err != nil {
		t.Fatalf("Patch delegated explicit deny policy: %v", err)
	}
	provider.failCall = func(op string) {
		t.Fatalf("remote delegated explicit deny reached provider as %q", op)
	}
	if _, err := broker.Invoke(ctx, p, "remote", "", "unary.op", nil); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("remote delegated explicit deny = %v, want ErrAuthorizationDenied", err)
	}
}

func TestAppOperationPolicyNilStoreAndNoAuthorizationProviderPreserveBaselineDevMode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	provider := newAppOperationPolicyProvider("workspace")
	p := appOperationPolicyPrincipal()
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		nil,
		nil,
	)

	result, err := broker.Invoke(ctx, p, "workspace", "", "unary.op", nil)
	if err != nil {
		t.Fatalf("Invoke with nil policy store and no authorization provider = %v, want baseline dev-mode allow", err)
	}
	requirePolicyResult(t, result, `{"operation":"unary.op"}`)
}

type appOperationPolicyProvider struct {
	*coretesting.StubIntegration
	sessionCatalog *catalog.Catalog
	failCall       func(string)
}

func newAppOperationPolicyProvider(name string) *appOperationPolicyProvider {
	provider := &appOperationPolicyProvider{}
	provider.StubIntegration = &coretesting.StubIntegration{
		N:        name,
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: name,
			Operations: []catalog.CatalogOperation{
				{ID: core.GraphQLCapabilityID, Method: "POST", AllowedRoles: []string{"admin"}},
				{
					ID:           "unary.op",
					Method:       "POST",
					Transport:    catalog.TransportApp,
					Description:  "static unary metadata",
					AllowedRoles: []string{"admin"},
				},
				{
					ID:           "stream.op",
					Method:       "GET",
					Transport:    catalog.TransportApp,
					Description:  "static stream metadata",
					AllowedRoles: []string{"admin"},
					Response: &catalog.OperationResponseSpec{
						Stream: &catalog.StreamResponseSpec{MediaType: "application/x-ndjson"},
					},
				},
				{
					ID:           "maybe.op",
					Method:       "POST",
					Transport:    catalog.TransportApp,
					Description:  "static maybe metadata",
					AllowedRoles: []string{"admin"},
				},
			},
		},
		ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
			if provider.failCall != nil {
				provider.failCall(op)
			}
			return &core.OperationResult{Status: 200, Body: []byte(fmt.Sprintf(`{"operation":%q}`, op))}, nil
		},
		StreamFn: func(_ context.Context, op string, _ map[string]any, _ string) (core.StreamReader, error) {
			if provider.failCall != nil {
				provider.failCall(op)
			}
			return &sliceCoreStreamReader{frames: []*core.InvokeFrame{
				{Metadata: &core.InvokeMetadata{Status: 200, MediaType: "application/x-ndjson"}},
				{Data: []byte(`{"ok":true}` + "\n")},
			}}, nil
		},
	}
	provider.sessionCatalog = &catalog.Catalog{
		Name: name,
		Operations: []catalog.CatalogOperation{{
			ID:           "dynamic.mcp",
			Method:       "POST",
			Transport:    catalog.TransportMCPPassthrough,
			Description:  "dynamic MCP metadata",
			AllowedRoles: []string{"admin"},
		}},
	}
	return provider
}

func (p *appOperationPolicyProvider) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	return p.sessionCatalog.Clone(), nil
}

func (p *appOperationPolicyProvider) InvokeGraphQL(context.Context, core.GraphQLRequest, string) (*core.OperationResult, error) {
	if p.failCall != nil {
		p.failCall(core.GraphQLCapabilityID)
	}
	return &core.OperationResult{Status: 200, Body: []byte(`{"data":{}}`)}, nil
}

type appOperationPolicyRemoteProvider struct {
	*appOperationPolicyProvider
}

func (p *appOperationPolicyRemoteProvider) RemoteCredentialDelegated() bool { return true }

type failingAppOperationPolicyStore struct {
	err error
}

func (s failingAppOperationPolicyStore) GetAppOperationPolicy(context.Context, string) (core.AppOperationPolicy, error) {
	return nil, s.err
}

func appOperationPolicyPrincipal() *principal.Principal {
	return &principal.Principal{
		SubjectID: principal.UserSubjectID("policy-user"),
		UserID:    "policy-user",
		Kind:      principal.KindUser,
	}
}

func appOperationPolicyAuthz(subjectID string, roles []string) *batchAuthorizationProvider {
	allow := make(map[string][]string)
	for _, op := range []string{"unary.op", "stream.op", "maybe.op", "graphql", "dynamic.mcp"} {
		allow[subjectID+"|"+op] = roles
	}
	return &batchAuthorizationProvider{allow: allow}
}

func resolvePolicyCatalog(t *testing.T, ctx context.Context, provider core.Provider, resolver TokenResolver, p *principal.Principal) *catalog.Catalog {
	t.Helper()
	cat, _, err := ResolveCatalogForTargetsWithMetadata(ctx, provider, "workspace", resolver, p, []CatalogResolutionTarget{{}}, true)
	if err != nil {
		t.Fatalf("ResolveCatalogForTargetsWithMetadata: %v", err)
	}
	return cat
}

func requirePolicyResult(t *testing.T, result *core.OperationResult, wantBody string) {
	t.Helper()
	if result == nil || result.Status != 200 || string(result.Body) != wantBody {
		t.Fatalf("operation result = %+v, want status 200 and body %q", result, wantBody)
	}
}

func requirePolicyStream(t *testing.T, reader core.StreamReader) {
	t.Helper()
	if reader == nil {
		t.Fatal("stream reader = nil")
	}
	frame, err := reader.Recv()
	if err != nil {
		t.Fatalf("Recv stream metadata: %v", err)
	}
	if frame == nil || frame.Metadata == nil {
		t.Fatalf("stream first frame = %+v, want metadata", frame)
	}
	if frame.Metadata.Status != 200 || frame.Metadata.MediaType != "application/x-ndjson" {
		t.Fatalf("stream metadata = %+v, want status 200 mediaType application/x-ndjson", frame.Metadata)
	}
	frame, err = reader.Recv()
	if err != nil {
		t.Fatalf("Recv stream data: %v", err)
	}
	if frame == nil || string(frame.Data) != `{"ok":true}`+"\n" {
		t.Fatalf("stream data frame = %+v, want ok JSON line", frame)
	}
	if _, err := reader.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("Recv stream EOF = %v, want EOF", err)
	}
}

func assertPolicyModeDenied(t *testing.T, ctx context.Context, broker *Broker, p *principal.Principal, modeName, op string) {
	t.Helper()
	var err error
	switch modeName {
	case "stream":
		_, err = broker.InvokeStream(ctx, p, "workspace", "", op, nil)
	case "maybe":
		_, err = broker.InvokeMaybeStream(ctx, p, "workspace", "", op, nil)
	case "graphql":
		_, err = broker.InvokeGraphQL(ctx, p, "workspace", "", core.GraphQLRequest{Document: "query { viewer { id } }"})
	default:
		_, err = broker.Invoke(ctx, p, "workspace", "", op, nil)
	}
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("%s denied invoke = %v, want ErrAuthorizationDenied", modeName, err)
	}
}

func requireDynamicPolicyListing(t *testing.T, ctx context.Context, provider core.Provider, broker *Broker, p *principal.Principal, wantPresent bool) {
	t.Helper()
	cat := resolvePolicyCatalog(t, ctx, provider, broker, p)
	filtered, err := FilterCatalogForPrincipal(ctx, cat, "workspace", p, broker)
	if err != nil {
		t.Fatalf("FilterCatalogForPrincipal: %v", err)
	}
	_, present := catalog.OperationByID(filtered, "dynamic.mcp")
	if present != wantPresent {
		t.Fatalf("dynamic.mcp listing present = %v, want %v; catalog: %+v", present, wantPresent, filtered)
	}
}

func requireCatalogOperation(t *testing.T, cat *catalog.Catalog, id string) catalog.CatalogOperation {
	t.Helper()
	if op, ok := catalog.OperationByID(cat, id); ok {
		return op
	}
	t.Fatalf("catalog missing operation %q: %+v", id, cat)
	return catalog.CatalogOperation{}
}
