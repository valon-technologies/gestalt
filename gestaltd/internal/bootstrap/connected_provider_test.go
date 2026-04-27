package bootstrap

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type connectedCapabilityProvider struct {
	postConnectMetadata map[string]string
}

func (p *connectedCapabilityProvider) Name() string        { return "slack" }
func (p *connectedCapabilityProvider) DisplayName() string { return "Slack" }
func (p *connectedCapabilityProvider) Description() string { return "Slack provider" }
func (p *connectedCapabilityProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeUser
}
func (p *connectedCapabilityProvider) AuthTypes() []string { return []string{"oauth"} }
func (p *connectedCapabilityProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *connectedCapabilityProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *connectedCapabilityProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *connectedCapabilityProvider) ConnectionForOperation(string) string        { return "" }
func (p *connectedCapabilityProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name: "slack",
		Operations: []catalog.CatalogOperation{{
			ID:     "viewer",
			Method: http.MethodGet,
		}},
	}
}
func (p *connectedCapabilityProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
}
func (p *connectedCapabilityProvider) CatalogForRequest(_ context.Context, _ string) (*catalog.Catalog, error) {
	return p.Catalog(), nil
}
func (p *connectedCapabilityProvider) InvokeGraphQL(_ context.Context, _ core.GraphQLRequest, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: `{"data":{"viewer":{"id":"U456"}}}`}, nil
}
func (p *connectedCapabilityProvider) AuthorizationURL(state string, _ []string) string {
	return "https://example.com/start?state=" + state
}
func (p *connectedCapabilityProvider) ExchangeCode(_ context.Context, _ string) (*core.TokenResponse, error) {
	return &core.TokenResponse{AccessToken: "access-token"}, nil
}
func (p *connectedCapabilityProvider) RefreshToken(_ context.Context, _ string) (*core.TokenResponse, error) {
	return &core.TokenResponse{AccessToken: "refreshed-token"}, nil
}
func (p *connectedCapabilityProvider) PostConnect(_ context.Context, _ *core.ExternalCredential) (map[string]string, error) {
	return p.postConnectMetadata, nil
}

func TestBindProviderConnectionPreservesPostConnectCapability(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"gestalt.external_identity.type": "slack_identity",
		"gestalt.external_identity.id":   "team:T123:user:U456",
	}
	prov := bindProviderConnection(&connectedCapabilityProvider{postConnectMetadata: want}, "default")

	if _, ok := prov.(core.OAuthProvider); !ok {
		t.Fatal("expected bound provider to preserve oauth support")
	}
	if _, ok := prov.(core.SessionCatalogProvider); !ok {
		t.Fatal("expected bound provider to preserve session catalog support")
	}
	if _, ok := prov.(core.GraphQLSurfaceInvoker); !ok {
		t.Fatal("expected bound provider to preserve graphql support")
	}
	if got := prov.ConnectionForOperation("viewer"); got != "default" {
		t.Fatalf("ConnectionForOperation(viewer) = %q, want %q", got, "default")
	}
	if !core.SupportsPostConnect(prov) {
		t.Fatal("expected bound provider to preserve post-connect support")
	}

	got, supported, err := core.PostConnect(context.Background(), prov, &core.ExternalCredential{
		Integration: "slack",
		Connection:  "default",
		AccessToken: "tok",
	})
	if err != nil {
		t.Fatalf("PostConnect: %v", err)
	}
	if !supported {
		t.Fatal("expected core.PostConnect to report support")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PostConnect metadata = %#v, want %#v", got, want)
	}
}

type connectedNoPostConnectProvider struct{}

func (p *connectedNoPostConnectProvider) Name() string        { return "slack" }
func (p *connectedNoPostConnectProvider) DisplayName() string { return "Slack" }
func (p *connectedNoPostConnectProvider) Description() string { return "Slack provider" }
func (p *connectedNoPostConnectProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeUser
}
func (p *connectedNoPostConnectProvider) AuthTypes() []string { return []string{"oauth"} }
func (p *connectedNoPostConnectProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *connectedNoPostConnectProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *connectedNoPostConnectProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *connectedNoPostConnectProvider) ConnectionForOperation(string) string        { return "" }
func (p *connectedNoPostConnectProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name: "slack",
		Operations: []catalog.CatalogOperation{{
			ID:     "viewer",
			Method: http.MethodGet,
		}},
	}
}
func (p *connectedNoPostConnectProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
}
func (p *connectedNoPostConnectProvider) CatalogForRequest(_ context.Context, _ string) (*catalog.Catalog, error) {
	return p.Catalog(), nil
}
func (p *connectedNoPostConnectProvider) InvokeGraphQL(_ context.Context, _ core.GraphQLRequest, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: `{"data":{"viewer":{"id":"U456"}}}`}, nil
}
func (p *connectedNoPostConnectProvider) AuthorizationURL(state string, _ []string) string {
	return "https://example.com/start?state=" + state
}
func (p *connectedNoPostConnectProvider) ExchangeCode(_ context.Context, _ string) (*core.TokenResponse, error) {
	return &core.TokenResponse{AccessToken: "access-token"}, nil
}
func (p *connectedNoPostConnectProvider) RefreshToken(_ context.Context, _ string) (*core.TokenResponse, error) {
	return &core.TokenResponse{AccessToken: "refreshed-token"}, nil
}

func TestBindProviderConnectionDoesNotFalsePositivePostConnectSupport(t *testing.T) {
	t.Parallel()

	prov := bindProviderConnection(&connectedNoPostConnectProvider{}, "default")
	if core.SupportsPostConnect(prov) {
		t.Fatal("expected bound provider to report no post-connect support")
	}

	got, supported, err := core.PostConnect(context.Background(), prov, &core.ExternalCredential{
		Integration: "slack",
		Connection:  "default",
		AccessToken: "tok",
	})
	if err != nil {
		t.Fatalf("PostConnect: %v", err)
	}
	if supported {
		t.Fatal("expected core.PostConnect to report unsupported")
	}
	if got != nil {
		t.Fatalf("PostConnect metadata = %#v, want nil", got)
	}
}
