package invocation_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	coreintegration "github.com/valon-technologies/gestalt/server/services/plugins/declarative"
	"github.com/valon-technologies/gestalt/server/services/testutil"
)

type stubProviderWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubProviderWithOps) Catalog() *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:       s.N,
		Operations: make([]catalog.CatalogOperation, 0, len(s.ops)),
	}
	for _, op := range s.ops {
		params := make([]catalog.CatalogParameter, 0, len(op.Parameters))
		for _, param := range op.Parameters {
			params = append(params, catalog.CatalogParameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
				Default:     param.Default,
			})
		}
		cat.Operations = append(cat.Operations, catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Path:        "/" + op.Name,
			Description: op.Description,
			Parameters:  params,
		})
	}
	coreintegration.CompileSchemas(cat)
	return cat
}

type capturingSink struct {
	entries []core.AuditEntry
}

func (s *capturingSink) Log(_ context.Context, entry core.AuditEntry) {
	s.entries = append(s.entries, entry)
}

type funcInvoker struct {
	invoke func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
}

func (f funcInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	return f.invoke(ctx, p, providerName, instance, operation, params)
}

type optionalInvoker struct {
	invoke              func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
	invokeGraphQL       func(ctx context.Context, p *principal.Principal, providerName, instance string, request invocation.GraphQLRequest) (*core.OperationResult, error)
	resolveToken        func(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error)
	resolveSubjectToken func(ctx context.Context, prov core.Provider, subjectID, providerName, connection, instance string) (context.Context, string, error)
}

func (o *optionalInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	if o.invoke != nil {
		return o.invoke(ctx, p, providerName, instance, operation, params)
	}
	return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
}

func (o *optionalInvoker) InvokeGraphQL(ctx context.Context, p *principal.Principal, providerName, instance string, request invocation.GraphQLRequest) (*core.OperationResult, error) {
	return o.invokeGraphQL(ctx, p, providerName, instance, request)
}

func (o *optionalInvoker) ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
	return o.resolveToken(ctx, p, providerName, connection, instance)
}

func (o *optionalInvoker) ResolveSubjectToken(ctx context.Context, prov core.Provider, subjectID, providerName, connection, instance string) (context.Context, string, error) {
	return o.resolveSubjectToken(ctx, prov, subjectID, providerName, connection, instance)
}

type contextMarkerKey struct{}

func guardTestProvider(name string) *stubProviderWithOps {
	return &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        name,
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: http.MethodGet}},
	}
}

func newGuardedTestInvoker(t *testing.T, providers ...core.Provider) *invocation.Broker {
	t.Helper()
	return invocation.NewBroker(testutil.NewProviderRegistry(t, providers...), nil, nil)
}

func TestGuardedInvoker_AllowedProvider(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithAllowedProviders([]string{"alpha"}), invocation.WithoutRateLimit())

	result, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.Status)
	}
}

func TestGuardedInvoker_DisallowedProvider(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithAllowedProviders([]string{"beta"}), invocation.WithoutRateLimit())

	_, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
	if err == nil {
		t.Fatal("expected error for disallowed provider")
	}
	if !strings.Contains(err.Error(), "not available in this scope") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGuardedInvoker_NoAllowlist(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"), guardTestProvider("beta"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithoutRateLimit())

	for _, provider := range []string{"alpha", "beta"} {
		result, err := guarded.Invoke(context.Background(), nil, provider, "", "ping", nil)
		if err != nil {
			t.Fatalf("Invoke %s: %v", provider, err)
		}
		if result.Status != http.StatusOK {
			t.Fatalf("expected 200, got %d", result.Status)
		}
	}
}

func TestGuardedInvoker_MaxDepthEnforced(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithMaxDepth(2), invocation.WithoutRateLimit())

	ctx := invocation.ContextWithMeta(context.Background(), &invocation.InvocationMeta{RequestID: "test-id", Depth: 2})
	_, err := guarded.Invoke(ctx, nil, "alpha", "", "ping", nil)
	if err == nil {
		t.Fatal("expected max depth error")
	}
	var maxDepthErr *invocation.MaxDepthError
	if !errors.As(err, &maxDepthErr) {
		t.Fatalf("expected MaxDepthError, got %T: %v", err, err)
	}
	if maxDepthErr.Depth != 2 || maxDepthErr.Max != 2 {
		t.Fatalf("expected depth=2 max=2, got depth=%d max=%d", maxDepthErr.Depth, maxDepthErr.Max)
	}
}

func TestGuardedInvoker_RecursionDetected(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithoutRateLimit())

	ctx := invocation.ContextWithMeta(context.Background(), &invocation.InvocationMeta{
		RequestID: "test-id",
		Depth:     1,
		CallChain: []string{"alpha/default/ping"},
	})
	_, err := guarded.Invoke(ctx, nil, "alpha", "", "ping", nil)
	if err == nil {
		t.Fatal("expected recursion error")
	}
	var recursionErr *invocation.RecursionError
	if !errors.As(err, &recursionErr) {
		t.Fatalf("expected RecursionError, got %T: %v", err, err)
	}
}

func TestGuardedInvoker_RateLimitExceeded(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithRateLimit(1, 1))

	if _, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil); err != nil {
		t.Fatalf("first Invoke: %v", err)
	}

	var rateLimited bool
	for i := 0; i < 10; i++ {
		if _, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil); err != nil {
			var rateLimitErr *invocation.RateLimitError
			if errors.As(err, &rateLimitErr) {
				rateLimited = true
				break
			}
			t.Fatalf("unexpected error type: %T: %v", err, err)
		}
	}
	if !rateLimited {
		t.Fatal("expected rate limit error after burst")
	}
}

func TestGuardedInvoker_AuditLogged(t *testing.T) {
	t.Parallel()

	sink := &capturingSink{}
	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "binding:my-hook", sink, invocation.WithAllowedProviders([]string{"alpha"}), invocation.WithoutRateLimit())

	_, _ = guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
	if len(sink.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(sink.entries))
	}
	entry := sink.entries[0]
	if entry.Source != "binding:my-hook" {
		t.Errorf("expected source binding:my-hook, got %q", entry.Source)
	}
	if entry.Provider != "alpha" {
		t.Errorf("expected provider alpha, got %q", entry.Provider)
	}
	if entry.Operation != "ping" {
		t.Errorf("expected operation ping, got %q", entry.Operation)
	}
	if !entry.Allowed {
		t.Error("expected Allowed=true")
	}
	if entry.RequestID == "" {
		t.Error("expected non-empty RequestID")
	}

	_, _ = guarded.Invoke(context.Background(), nil, "beta", "", "ping", nil)
	if len(sink.entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(sink.entries))
	}
	denied := sink.entries[1]
	if denied.Allowed {
		t.Error("expected denied call to be logged with Allowed=false")
	}
	if denied.Error == "" {
		t.Error("expected denied call to include an error")
	}
}

func TestGuardedInvoker_AuditLoggedOnPanic(t *testing.T) {
	t.Parallel()

	sink := &capturingSink{}
	guarded := invocation.NewGuarded(funcInvoker{
		invoke: func(ctx context.Context, _ *principal.Principal, _ string, _ string, _ string, _ map[string]any) (*core.OperationResult, error) {
			invocation.SetCredentialAudit(ctx, core.ConnectionModeUser, "system:config", "workspace", "team-a")
			panic("boom")
		},
	}, nil, "test", sink, invocation.WithoutRateLimit())

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		if r != "boom" {
			t.Fatalf("unexpected panic: %v", r)
		}
		if len(sink.entries) != 1 {
			t.Fatalf("expected 1 audit entry, got %d", len(sink.entries))
		}
		entry := sink.entries[0]
		if !entry.Allowed {
			t.Fatal("expected panicing invocation to keep allowed=true audit entry")
		}
		if entry.CredentialMode != string(core.ConnectionModeUser) {
			t.Fatalf("expected credential mode identity, got %q", entry.CredentialMode)
		}
		if entry.CredentialSubjectID != "system:config" {
			t.Fatalf("expected credential subject %q, got %q", "system:config", entry.CredentialSubjectID)
		}
		if entry.CredentialConnection != "workspace" {
			t.Fatalf("expected credential connection workspace, got %q", entry.CredentialConnection)
		}
		if entry.CredentialInstance != "team-a" {
			t.Fatalf("expected credential instance team-a, got %q", entry.CredentialInstance)
		}
	}()

	_, _ = guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
}

func TestGuardedInvoker_ListCapabilities(t *testing.T) {
	t.Parallel()

	alpha := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "alpha"},
		ops: []core.Operation{
			{Name: "op1", Description: "Alpha op 1", Method: http.MethodGet},
			{Name: "op2", Description: "Alpha op 2", Method: http.MethodPost},
		},
	}
	beta := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "beta"},
		ops: []core.Operation{
			{Name: "op3", Description: "Beta op 3", Method: http.MethodGet},
		},
	}

	base := newGuardedTestInvoker(t, alpha, beta)

	t.Run("with allowlist", func(t *testing.T) {
		t.Parallel()
		guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithAllowedProviders([]string{"alpha"}))
		caps := guarded.ListCapabilities()
		if len(caps) != 2 {
			t.Fatalf("expected 2 capabilities, got %d", len(caps))
		}
		for _, cap := range caps {
			if cap.Provider != "alpha" {
				t.Fatalf("expected provider alpha, got %q", cap.Provider)
			}
		}
	})

	t.Run("without allowlist", func(t *testing.T) {
		t.Parallel()
		guarded := invocation.NewGuarded(base, base, "test", &capturingSink{})
		caps := guarded.ListCapabilities()
		if len(caps) != 3 {
			t.Fatalf("expected 3 capabilities, got %d", len(caps))
		}
	})
}

func TestGuardedInvoker_OptionalGraphQLDelegates(t *testing.T) {
	t.Parallel()

	sink := &capturingSink{}
	base := &optionalInvoker{
		invokeGraphQL: func(ctx context.Context, p *principal.Principal, providerName, instance string, request invocation.GraphQLRequest) (*core.OperationResult, error) {
			if p.SubjectID != "subject-1" {
				t.Fatalf("expected subject-1, got %q", p.SubjectID)
			}
			if providerName != "alpha" {
				t.Fatalf("expected provider alpha, got %q", providerName)
			}
			if instance != "team-a" {
				t.Fatalf("expected instance team-a, got %q", instance)
			}
			if request.Document != "query Test { viewer { id } }" {
				t.Fatalf("unexpected document: %q", request.Document)
			}
			if request.Variables["limit"] != 3 {
				t.Fatalf("unexpected variables: %#v", request.Variables)
			}
			meta := invocation.MetaFromContext(ctx)
			if meta == nil {
				t.Fatal("expected invocation metadata")
			}
			if meta.RequestID != "req-1" {
				t.Fatalf("expected request req-1, got %q", meta.RequestID)
			}
			if meta.Depth != 2 {
				t.Fatalf("expected depth 2, got %d", meta.Depth)
			}
			wantChain := []string{"root/default/ping", "alpha/team-a/graphql"}
			if strings.Join(meta.CallChain, ",") != strings.Join(wantChain, ",") {
				t.Fatalf("expected call chain %v, got %v", wantChain, meta.CallChain)
			}
			return &core.OperationResult{Status: http.StatusAccepted, Body: `{"graphql":true}`}, nil
		},
	}
	guarded := invocation.NewGuarded(base, nil, "test", sink, invocation.WithAllowedProviders([]string{"alpha"}), invocation.WithoutRateLimit())

	ctx := invocation.ContextWithMeta(context.Background(), &invocation.InvocationMeta{
		RequestID: "req-1",
		Depth:     1,
		CallChain: []string{"root/default/ping"},
	})
	result, err := guarded.InvokeGraphQL(ctx, &principal.Principal{SubjectID: "subject-1"}, "alpha", "team-a", invocation.GraphQLRequest{
		Document:  "query Test { viewer { id } }",
		Variables: map[string]any{"limit": 3},
	})
	if err != nil {
		t.Fatalf("InvokeGraphQL: %v", err)
	}
	if result.Status != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", result.Status)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(sink.entries))
	}
	entry := sink.entries[0]
	if entry.Provider != "alpha" || entry.Operation != "graphql" || !entry.Allowed {
		t.Fatalf("unexpected audit entry: %#v", entry)
	}
}

func TestGuardedInvoker_OptionalTokenResolversDelegateOutsideGuardChecks(t *testing.T) {
	t.Parallel()

	prov := guardTestProvider("alpha")
	base := &optionalInvoker{
		resolveToken: func(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
			if p.SubjectID != "subject-1" {
				t.Fatalf("expected subject-1, got %q", p.SubjectID)
			}
			if providerName != "alpha" || connection != "workspace" || instance != "team-a" {
				t.Fatalf("unexpected token target: %s/%s/%s", providerName, connection, instance)
			}
			return context.WithValue(ctx, contextMarkerKey{}, "token"), "token-123", nil
		},
		resolveSubjectToken: func(ctx context.Context, gotProv core.Provider, subjectID, providerName, connection, instance string) (context.Context, string, error) {
			if gotProv != prov {
				t.Fatal("expected provider to be forwarded")
			}
			if subjectID != "subject-2" || providerName != "alpha" || connection != "workspace" || instance != "team-b" {
				t.Fatalf("unexpected subject token target: %s/%s/%s/%s", subjectID, providerName, connection, instance)
			}
			return context.WithValue(ctx, contextMarkerKey{}, "subject-token"), "subject-token-456", nil
		},
	}
	guarded := invocation.NewGuarded(base, nil, "test", &capturingSink{}, invocation.WithAllowedProviders([]string{"other"}), invocation.WithoutRateLimit())

	tokenCtx, token, err := guarded.ResolveToken(context.Background(), &principal.Principal{SubjectID: "subject-1"}, "alpha", "workspace", "team-a")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if token != "token-123" {
		t.Fatalf("expected token-123, got %q", token)
	}
	if tokenCtx.Value(contextMarkerKey{}) != "token" {
		t.Fatalf("expected token context marker, got %#v", tokenCtx.Value(contextMarkerKey{}))
	}

	subjectCtx, subjectToken, err := guarded.ResolveSubjectToken(context.Background(), prov, "subject-2", "alpha", "workspace", "team-b")
	if err != nil {
		t.Fatalf("ResolveSubjectToken: %v", err)
	}
	if subjectToken != "subject-token-456" {
		t.Fatalf("expected subject-token-456, got %q", subjectToken)
	}
	if subjectCtx.Value(contextMarkerKey{}) != "subject-token" {
		t.Fatalf("expected subject-token context marker, got %#v", subjectCtx.Value(contextMarkerKey{}))
	}
}

func TestGuardedInvoker_OptionalDependenciesUnsupported(t *testing.T) {
	t.Parallel()

	var invoked bool
	sink := &capturingSink{}
	base := funcInvoker{
		invoke: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
			invoked = true
			return &core.OperationResult{Status: http.StatusOK}, nil
		},
	}
	guarded := invocation.NewGuarded(base, nil, "test", sink, invocation.WithAllowedProviders([]string{"other"}), invocation.WithoutRateLimit())

	_, err := guarded.InvokeGraphQL(context.Background(), &principal.Principal{}, "alpha", "", invocation.GraphQLRequest{})
	if err == nil || err.Error() != "plugin invoker is not available" {
		t.Fatalf("expected plugin invoker unavailable error, got %v", err)
	}
	if invoked {
		t.Fatal("plain invoker should not be called for unsupported GraphQL")
	}
	if len(sink.entries) != 0 {
		t.Fatalf("expected no audit entries for unsupported GraphQL, got %d", len(sink.entries))
	}

	ctx := context.WithValue(context.Background(), contextMarkerKey{}, "original")
	tokenCtx, token, err := guarded.ResolveToken(ctx, &principal.Principal{}, "alpha", "workspace", "team-a")
	if err == nil || err.Error() != "token resolution not supported" {
		t.Fatalf("expected token unsupported error, got %v", err)
	}
	if token != "" {
		t.Fatalf("expected empty token, got %q", token)
	}
	if tokenCtx != ctx {
		t.Fatal("expected unsupported token resolution to return original context")
	}

	subjectCtx, subjectToken, err := guarded.ResolveSubjectToken(ctx, guardTestProvider("alpha"), "subject-1", "alpha", "workspace", "team-a")
	if err == nil || err.Error() != "subject token resolution not supported" {
		t.Fatalf("expected subject token unsupported error, got %v", err)
	}
	if subjectToken != "" {
		t.Fatalf("expected empty subject token, got %q", subjectToken)
	}
	if subjectCtx != ctx {
		t.Fatal("expected unsupported subject token resolution to return original context")
	}
}
