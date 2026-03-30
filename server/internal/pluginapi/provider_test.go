package pluginapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	sdkpluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
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

func (p *roundTripProvider) ListOperations() []core.Operation {
	return []core.Operation{
		{
			Name:        "echo",
			Description: "Echo input",
			Method:      http.MethodPost,
			Parameters: []core.Parameter{
				{Name: "message", Type: "string", Description: "message", Required: true, Default: "hello"},
			},
		},
	}
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
			{ID: "echo", Method: http.MethodPost, Path: "/echo", Transport: catalog.TransportREST},
		},
	}
}

func (p *roundTripProvider) CatalogForRequest(_ context.Context, token string) (*catalog.Catalog, error) {
	return &catalog.Catalog{
		Name:        "roundtrip-session",
		DisplayName: token,
		Description: "session catalog",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost, Path: "/echo", Transport: catalog.TransportREST},
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

func (p *manualOnlySDKProvider) ConnectionMode() sdkpluginsdk.ConnectionMode {
	return sdkpluginsdk.ConnectionModeIdentity
}

func (p *manualOnlySDKProvider) ListOperations() []sdkpluginsdk.Operation { return nil }

func (p *manualOnlySDKProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*sdkpluginsdk.OperationResult, error) {
	return &sdkpluginsdk.OperationResult{Status: 200, Body: `{}`}, nil
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
	if _, ok := prov.(core.CatalogProvider); !ok {
		t.Fatal("expected remote provider to implement CatalogProvider")
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

	ctx := core.WithConnectionParams(context.Background(), map[string]string{"tenant": "acme"})
	result, err := prov.Execute(ctx, "echo", map[string]any{"message": "hi"}, "secret-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != 201 || result.Body != "echo||hi|acme" {
		t.Fatalf("unexpected execute result: %+v", result)
	}

	cp := prov.(core.CatalogProvider)
	if cat := cp.Catalog(); cat == nil || cat.Name != "roundtrip" {
		t.Fatalf("unexpected static catalog: %+v", cat)
	}

	scp := prov.(core.SessionCatalogProvider)
	sessionCat, err := scp.CatalogForRequest(context.Background(), "token-123")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if sessionCat.Name != "roundtrip-session" || sessionCat.DisplayName != "" {
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

	t.Run("no icon and no catalog returns nil", func(t *testing.T) {
		t.Parallel()

		client := newProviderPluginClient(t, sdkpluginsdk.NewProviderServer(&manualOnlySDKProvider{}))
		prov, err := NewRemoteProvider(context.Background(), client, "manual-only", nil)
		if err != nil {
			t.Fatalf("NewRemoteProvider: %v", err)
		}
		cp, ok := prov.(core.CatalogProvider)
		if !ok {
			t.Fatal("expected provider to implement CatalogProvider")
		}
		if cat := cp.Catalog(); cat != nil {
			t.Fatalf("expected nil catalog, got %+v", cat)
		}
	})

	t.Run("SetIconSVG injects icon when no static catalog", func(t *testing.T) {
		t.Parallel()

		client := newProviderPluginClient(t, sdkpluginsdk.NewProviderServer(&manualOnlySDKProvider{}))
		prov, err := NewRemoteProvider(context.Background(), client, "manual-only", nil)
		if err != nil {
			t.Fatalf("NewRemoteProvider: %v", err)
		}
		base := prov.(*remoteProviderBase)
		base.SetIconSVG(testSVG)

		cp := prov.(core.CatalogProvider)
		cat := cp.Catalog()
		if cat == nil {
			t.Fatal("expected non-nil catalog after SetIconSVG")
		}
		if cat.IconSVG != testSVG {
			t.Fatalf("IconSVG = %q, want %q", cat.IconSVG, testSVG)
		}
	})

	t.Run("SetIconSVG fills empty icon on existing catalog", func(t *testing.T) {
		t.Parallel()

		client := newProviderPluginClient(t, NewProviderServer(&roundTripProvider{}))
		prov, err := NewRemoteProvider(context.Background(), client, "roundtrip", nil)
		if err != nil {
			t.Fatalf("NewRemoteProvider: %v", err)
		}
		cp := prov.(core.CatalogProvider)
		if cp.Catalog().IconSVG != "" {
			t.Fatal("expected empty icon before SetIconSVG")
		}

		base := prov.(*remoteProviderWithSessionCatalog).remoteProviderBase
		base.SetIconSVG(testSVG)

		cat := cp.Catalog()
		if cat.IconSVG != testSVG {
			t.Fatalf("IconSVG = %q, want %q", cat.IconSVG, testSVG)
		}
	})

	t.Run("existing catalog icon is preserved", func(t *testing.T) {
		t.Parallel()

		const existingIcon = `<svg><circle/></svg>`
		client := newProviderPluginClient(t, NewProviderServer(&roundTripProvider{}))
		prov, err := NewRemoteProvider(context.Background(), client, "roundtrip", nil)
		if err != nil {
			t.Fatalf("NewRemoteProvider: %v", err)
		}
		base := prov.(*remoteProviderWithSessionCatalog).remoteProviderBase
		base.catalog.IconSVG = existingIcon
		base.SetIconSVG(testSVG)

		cp := prov.(core.CatalogProvider)
		cat := cp.Catalog()
		if cat.IconSVG != existingIcon {
			t.Fatalf("IconSVG = %q, want preserved %q", cat.IconSVG, existingIcon)
		}
	})
}

func TestRemoteProviderManualAuthOnly(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, sdkpluginsdk.NewProviderServer(&manualOnlySDKProvider{}))
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

}
