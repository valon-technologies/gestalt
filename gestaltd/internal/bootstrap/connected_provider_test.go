package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type connectedCapabilityProvider struct {
	operationConnection map[string]string
	resolveConnection   func(operation string, params map[string]any) (string, error)
	overrideAllowed     func(operation string, params map[string]any) bool
}

func (p *connectedCapabilityProvider) Name() string        { return "test" }
func (p *connectedCapabilityProvider) DisplayName() string { return "Test" }
func (p *connectedCapabilityProvider) Description() string { return "Test provider" }
func (p *connectedCapabilityProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeSubject
}
func (p *connectedCapabilityProvider) AuthTypes() []string { return []string{"oauth"} }
func (p *connectedCapabilityProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *connectedCapabilityProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *connectedCapabilityProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *connectedCapabilityProvider) ConnectionForOperation(operation string) string {
	return p.operationConnection[operation]
}
func (p *connectedCapabilityProvider) ResolveConnectionForOperation(operation string, params map[string]any) (string, error) {
	if p.resolveConnection != nil {
		return p.resolveConnection(operation, params)
	}
	return p.ConnectionForOperation(operation), nil
}
func (p *connectedCapabilityProvider) OperationConnectionOverrideAllowed(operation string, params map[string]any) bool {
	if p.overrideAllowed != nil {
		return p.overrideAllowed(operation, params)
	}
	return false
}
func (p *connectedCapabilityProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name: "test",
		Operations: []catalog.CatalogOperation{{
			ID:     "viewer",
			Method: http.MethodGet,
		}},
	}
}
func (p *connectedCapabilityProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
}
func (p *connectedCapabilityProvider) CatalogForRequest(_ context.Context, _ string) (*catalog.Catalog, error) {
	return p.Catalog(), nil
}
func (p *connectedCapabilityProvider) InvokeGraphQL(_ context.Context, _ core.GraphQLRequest, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"data":{"viewer":{"id":"U456"}}}`)}, nil
}
func (p *connectedCapabilityProvider) AuthorizationURL(state string, _ []string) string {
	return "https://example.com/start?state=" + state
}
func (p *connectedCapabilityProvider) ExchangeCode(_ context.Context, _ string) (*core.OAuthTokenResponse, error) {
	return &core.OAuthTokenResponse{AccessToken: "access-token"}, nil
}
func (p *connectedCapabilityProvider) RefreshToken(_ context.Context, _ string) (*core.OAuthTokenResponse, error) {
	return &core.OAuthTokenResponse{AccessToken: "refreshed-token"}, nil
}
func TestBindProviderConnectionResolvesInnerStaticOperationConnection(t *testing.T) {
	t.Parallel()

	prov := bindProviderConnection(&connectedCapabilityProvider{
		operationConnection: map[string]string{"viewer": "bot"},
	}, "default")

	if got := prov.ConnectionForOperation("viewer"); got != "bot" {
		t.Fatalf("ConnectionForOperation(viewer) = %q, want %q", got, "bot")
	}
	got, err := invocation.ResolveOperationConnection(prov, "viewer", nil)
	if err != nil {
		t.Fatalf("ResolveOperationConnection(viewer): %v", err)
	}
	if got != "bot" {
		t.Fatalf("ResolveOperationConnection(viewer) = %q, want %q", got, "bot")
	}
}

func TestBindProviderConnectionPreservesOperationConnectionInterfacesThroughCapabilityWrappers(t *testing.T) {
	t.Parallel()

	prov := bindProviderConnection(&connectedCapabilityProvider{
		operationConnection: map[string]string{"viewer": "default"},
		resolveConnection: func(operation string, params map[string]any) (string, error) {
			if params["actor"] == "bot" {
				return "bot", nil
			}
			return "default", nil
		},
		overrideAllowed: func(operation string, params map[string]any) bool {
			return params["allow_override"] == true
		},
	}, "default")

	resolver, ok := prov.(core.OperationConnectionResolver)
	if !ok {
		t.Fatal("expected bound provider to preserve operation connection resolver")
	}
	got, err := resolver.ResolveConnectionForOperation("viewer", map[string]any{"actor": "bot"})
	if err != nil {
		t.Fatalf("ResolveConnectionForOperation(viewer): %v", err)
	}
	if got != "bot" {
		t.Fatalf("ResolveConnectionForOperation(viewer) = %q, want %q", got, "bot")
	}

	policy, ok := prov.(core.OperationConnectionOverridePolicy)
	if !ok {
		t.Fatal("expected bound provider to preserve operation connection override policy")
	}
	if !policy.OperationConnectionOverrideAllowed("viewer", map[string]any{"allow_override": true}) {
		t.Fatal("expected operation connection override policy to be forwarded")
	}
}

type connectedBasicProvider struct{}

func (p *connectedBasicProvider) Name() string        { return "test" }
func (p *connectedBasicProvider) DisplayName() string { return "Test" }
func (p *connectedBasicProvider) Description() string { return "Test provider" }
func (p *connectedBasicProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeSubject
}
func (p *connectedBasicProvider) AuthTypes() []string { return []string{"oauth"} }
func (p *connectedBasicProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *connectedBasicProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *connectedBasicProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *connectedBasicProvider) ConnectionForOperation(string) string        { return "" }
func (p *connectedBasicProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name: "test",
		Operations: []catalog.CatalogOperation{{
			ID:     "viewer",
			Method: http.MethodGet,
		}},
	}
}
func (p *connectedBasicProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
}
func (p *connectedBasicProvider) CatalogForRequest(_ context.Context, _ string) (*catalog.Catalog, error) {
	return p.Catalog(), nil
}
func (p *connectedBasicProvider) InvokeGraphQL(_ context.Context, _ core.GraphQLRequest, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"data":{"viewer":{"id":"U456"}}}`)}, nil
}
func (p *connectedBasicProvider) AuthorizationURL(state string, _ []string) string {
	return "https://example.com/start?state=" + state
}
func (p *connectedBasicProvider) ExchangeCode(_ context.Context, _ string) (*core.OAuthTokenResponse, error) {
	return &core.OAuthTokenResponse{AccessToken: "access-token"}, nil
}
func (p *connectedBasicProvider) RefreshToken(_ context.Context, _ string) (*core.OAuthTokenResponse, error) {
	return &core.OAuthTokenResponse{AccessToken: "refreshed-token"}, nil
}

type connectedNoSessionCatalogProvider struct {
	connectedBasicProvider
	catalogCalls int
}

func (p *connectedNoSessionCatalogProvider) SupportsSessionCatalog() bool {
	return false
}

func (p *connectedNoSessionCatalogProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	p.catalogCalls++
	return p.connectedBasicProvider.CatalogForRequest(ctx, token)
}

type connectedStaticHeaderProvider struct {
	connectedBasicProvider
	staticHeaders map[string]string
}

func (p *connectedStaticHeaderProvider) StaticHeaders() map[string]string {
	return p.staticHeaders
}

func TestBindProviderConnectionForwardsStaticHeaders(t *testing.T) {
	t.Parallel()

	inner := &connectedStaticHeaderProvider{
		staticHeaders: map[string]string{
			"X-Tenant-Sid": "TENDefault",
		},
	}
	prov := bindProviderConnection(inner, "dev")

	hp, ok := prov.(interface{ StaticHeaders() map[string]string })
	if !ok {
		t.Fatal("expected bound provider to expose StaticHeaders")
	}
	headers := hp.StaticHeaders()
	if headers["X-Tenant-Sid"] != "TENDefault" {
		t.Fatalf("StaticHeaders() = %#v, want default tenant header", headers)
	}
}

func TestBindProviderConnectionDoesNotFalsePositiveSessionCatalogSupport(t *testing.T) {
	t.Parallel()

	inner := &connectedNoSessionCatalogProvider{}
	prov := bindProviderConnection(inner, "default")
	if core.SupportsSessionCatalog(prov) {
		t.Fatal("expected bound provider to report no session catalog support")
	}

	cat, attempted, err := core.CatalogForRequest(context.Background(), prov, "tok")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if attempted {
		t.Fatal("expected core.CatalogForRequest to report no attempt")
	}
	if cat != nil {
		t.Fatalf("CatalogForRequest catalog = %#v, want nil", cat)
	}
	if inner.catalogCalls != 0 {
		t.Fatalf("CatalogForRequest calls = %d, want 0", inner.catalogCalls)
	}

	scp, ok := prov.(core.SessionCatalogProvider)
	if !ok {
		t.Fatal("expected outer OAuth/GraphQL wrapper to still have direct CatalogForRequest method")
	}
	_, err = scp.CatalogForRequest(context.Background(), "tok")
	if !errors.Is(err, core.ErrSessionCatalogUnsupported) {
		t.Fatalf("direct CatalogForRequest error = %v, want ErrSessionCatalogUnsupported", err)
	}
	if inner.catalogCalls != 0 {
		t.Fatalf("direct CatalogForRequest calls = %d, want 0", inner.catalogCalls)
	}
}
