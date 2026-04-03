package pluginhost

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	sdkgestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type roundTripProvider struct{}

func (p *roundTripProvider) Name() string        { return "roundtrip" }
func (p *roundTripProvider) DisplayName() string { return "Round Trip" }
func (p *roundTripProvider) Description() string { return "test provider" }
func (p *roundTripProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeEither
}

func (p *roundTripProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	return &core.OperationResult{
		Status: 201,
		Body:   fmt.Sprintf("%s|%s|%s|%s", operation, token, params["message"], core.ConnectionParams(ctx)["tenant"]),
	}, nil
}

func (p *roundTripProvider) SupportsManualAuth() bool { return true }

func (p *roundTripProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name:        "roundtrip",
		DisplayName: "Round Trip",
		Description: "test provider",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	}
}

func (p *roundTripProvider) CatalogForRequest(_ context.Context, token string) (*catalog.Catalog, error) {
	return &catalog.Catalog{
		Name:        "roundtrip-session",
		DisplayName: token,
		Description: "session catalog",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	}, nil
}

func (p *roundTripProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return map[string]core.ConnectionParamDef{
		"tenant":  {Required: true, Description: "Tenant slug"},
		"team_id": {From: "token_response", Field: "team_id"},
	}
}

func (p *roundTripProvider) AuthTypes() []string {
	return []string{"manual"}
}

type manualOnlySDKProvider struct{}

func (p *manualOnlySDKProvider) Name() string { return "manual-only" }

func (p *manualOnlySDKProvider) DisplayName() string { return "Manual Only" }

func (p *manualOnlySDKProvider) Description() string { return "manual auth provider" }

func (p *manualOnlySDKProvider) ConnectionMode() sdkgestalt.ConnectionMode {
	return sdkgestalt.ConnectionModeIdentity
}

func (p *manualOnlySDKProvider) Catalog() *sdkgestalt.Catalog {
	return &sdkgestalt.Catalog{
		Name:        "manual-only",
		DisplayName: "Manual Only",
		Description: "manual auth provider",
		Operations: []sdkgestalt.CatalogOperation{
			{
				ID:          "echo",
				Description: "Echo input",
				Method:      http.MethodPost,
				Parameters: []sdkgestalt.CatalogParameter{
					{Name: "message", Type: "string", Description: "message", Required: true, Default: "hello"},
				},
			},
		},
	}
}

func (p *manualOnlySDKProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*sdkgestalt.OperationResult, error) {
	return &sdkgestalt.OperationResult{Status: 200, Body: `{}`}, nil
}

func (p *manualOnlySDKProvider) SupportsManualAuth() bool { return true }

func TestRemoteProviderRoundTrip(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, NewProviderServer(&roundTripProvider{}))
	prov, err := NewRemoteProvider(context.Background(), client, "roundtrip", nil)
	if err != nil {
		t.Fatalf("NewRemoteProvider: %v", err)
	}

	if prov.Name() != "roundtrip" {
		t.Fatalf("unexpected provider name: %q", prov.Name())
	}
	if prov.DisplayName() != "Round Trip" {
		t.Fatalf("unexpected display name: %q", prov.DisplayName())
	}
	if prov.ConnectionMode() != core.ConnectionModeEither {
		t.Fatalf("unexpected connection mode: %q", prov.ConnectionMode())
	}

	if _, ok := prov.(core.ManualProvider); !ok {
		t.Fatal("expected remote provider to implement ManualProvider")
	}
	if _, ok := prov.(core.SessionCatalogProvider); !ok {
		t.Fatal("expected remote provider to implement SessionCatalogProvider")
	}
	if _, ok := prov.(core.ConnectionParamProvider); !ok {
		t.Fatal("expected remote provider to implement ConnectionParamProvider")
	}
	if _, ok := prov.(core.AuthTypeLister); !ok {
		t.Fatal("expected remote provider to implement AuthTypeLister")
	}
	if cat := prov.Catalog(); cat == nil || len(cat.Operations) != 1 || cat.Operations[0].ID != "echo" {
		t.Fatalf("unexpected Catalog result: %+v", cat)
	}

	ctx := core.WithConnectionParams(context.Background(), map[string]string{"tenant": "acme"})
	result, err := prov.Execute(ctx, "echo", map[string]any{"message": "hi"}, "secret-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != 201 || result.Body != "echo|secret-token|hi|acme" {
		t.Fatalf("unexpected execute result: %+v", result)
	}

	if cat := prov.Catalog(); cat == nil || cat.Name != "roundtrip" {
		t.Fatalf("unexpected static catalog: %+v", cat)
	}

	scp := prov.(core.SessionCatalogProvider)
	sessionCat, err := scp.CatalogForRequest(context.Background(), "token-123")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if sessionCat.Name != "roundtrip-session" || sessionCat.DisplayName != "token-123" {
		t.Fatalf("unexpected session catalog: %+v", sessionCat)
	}

	cpp := prov.(core.ConnectionParamProvider)
	if defs := cpp.ConnectionParamDefs(); defs["tenant"].Description != "Tenant slug" || defs["team_id"].Field != "team_id" {
		t.Fatalf("unexpected connection param defs: %+v", defs)
	}

}

func TestRemoteProviderIconSVG(t *testing.T) {
	t.Parallel()

	const testSVG = `<svg xmlns="http://www.w3.org/2000/svg"><rect width="16" height="16"/></svg>`

	t.Run("metadata override fills empty icon on existing catalog", func(t *testing.T) {
		t.Parallel()

		client := newProviderPluginClient(t, NewProviderServer(&roundTripProvider{}))
		prov, err := NewRemoteProvider(context.Background(), client, "roundtrip", nil, WithMetadataOverrides("", "", testSVG))
		if err != nil {
			t.Fatalf("NewRemoteProvider: %v", err)
		}
		cat := prov.Catalog()
		if cat.IconSVG != testSVG {
			t.Fatalf("IconSVG = %q, want %q", cat.IconSVG, testSVG)
		}
	})

	t.Run("metadata override replaces existing catalog icon", func(t *testing.T) {
		t.Parallel()

		const existingIcon = `<svg><circle/></svg>`
		client := newProviderPluginClient(t, NewProviderServer(&roundTripProvider{}))
		prov, err := NewRemoteProvider(context.Background(), client, "roundtrip", nil, WithMetadataOverrides("", "", testSVG))
		if err != nil {
			t.Fatalf("NewRemoteProvider: %v", err)
		}
		base := prov.(*remoteProviderWithSessionCatalog).remoteProviderBase
		base.catalog.IconSVG = existingIcon

		cat := prov.Catalog()
		if cat.IconSVG != testSVG {
			t.Fatalf("IconSVG = %q, want override %q", cat.IconSVG, testSVG)
		}
	})
}

func TestRemoteProviderMetadataOverridesApplyToSessionCatalog(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, NewProviderServer(&roundTripProvider{}))
	prov, err := NewRemoteProvider(
		context.Background(),
		client,
		"roundtrip",
		nil,
		WithMetadataOverrides("Override", "Override description", "<svg/>"),
	)
	if err != nil {
		t.Fatalf("NewRemoteProvider: %v", err)
	}

	cat, err := prov.(core.SessionCatalogProvider).CatalogForRequest(context.Background(), "token-123")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if cat.DisplayName != "Override" {
		t.Fatalf("DisplayName = %q, want %q", cat.DisplayName, "Override")
	}
	if cat.Description != "Override description" {
		t.Fatalf("Description = %q, want %q", cat.Description, "Override description")
	}
	if cat.IconSVG != "<svg/>" {
		t.Fatalf("IconSVG = %q, want %q", cat.IconSVG, "<svg/>")
	}
}

func TestRemoteProviderManualAuthOnly(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, sdkgestalt.NewProviderServer(&manualOnlySDKProvider{}))
	prov, err := NewRemoteProvider(context.Background(), client, "manual-only", nil)
	if err != nil {
		t.Fatalf("NewRemoteProvider: %v", err)
	}

	mp, ok := prov.(core.ManualProvider)
	if !ok {
		t.Fatal("expected remote provider to implement core.ManualProvider")
	}
	if !mp.SupportsManualAuth() {
		t.Fatal("expected SupportsManualAuth() == true")
	}
	cat := prov.Catalog()
	if cat == nil || len(cat.Operations) != 1 {
		t.Fatalf("unexpected catalog: %+v", cat)
	}
	if cat.Operations[0].Transport != catalog.TransportPlugin {
		t.Fatalf("Transport = %q, want %q", cat.Operations[0].Transport, catalog.TransportPlugin)
	}
}
