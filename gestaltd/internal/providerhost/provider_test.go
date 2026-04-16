package providerhost

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	sdkgestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type roundTripProvider struct{ core.NoOAuth }

func (p *roundTripProvider) Configure(_ context.Context, _ string, _ map[string]any) error {
	return nil
}
func (p *roundTripProvider) Name() string                        { return "roundtrip" }
func (p *roundTripProvider) DisplayName() string                 { return "Round Trip" }
func (p *roundTripProvider) Description() string                 { return "test provider" }
func (p *roundTripProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeEither }
func (p *roundTripProvider) AuthTypes() []string                 { return []string{"manual"} }
func (p *roundTripProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return map[string]core.ConnectionParamDef{
		"tenant":  {Required: true, Description: "Tenant slug"},
		"team_id": {From: "token_response", Field: "team_id"},
	}
}
func (p *roundTripProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *roundTripProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *roundTripProvider) ConnectionForOperation(string) string        { return "" }

func (p *roundTripProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	subjectID := ""
	subjectKind := ""
	authSource := ""
	displayName := ""
	identityPresent := "false"
	if p := principal.FromContext(ctx); p != nil {
		subjectID = p.SubjectID
		if subjectID == "" && p.UserID != "" {
			subjectID = principal.UserSubjectID(p.UserID)
		}
		subjectKind = string(p.Kind)
		authSource = p.AuthSource()
		displayName = p.DisplayName
		if p.Identity != nil {
			identityPresent = "true"
		}
	}
	credential := invocation.CredentialContextFromContext(ctx)
	access := invocation.AccessContextFromContext(ctx)
	return &core.OperationResult{
		Status: 201,
		Body:   fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s", operation, token, params["message"], core.ConnectionParams(ctx)["tenant"], subjectID, subjectKind, displayName, identityPresent, authSource, credential.Mode, credential.SubjectID, access.Policy, access.Role),
	}, nil
}

func roundTripOperation(allowedRoles ...string) catalog.CatalogOperation {
	return catalog.CatalogOperation{
		ID:           "echo",
		ProviderID:   "queries",
		Method:       http.MethodPost,
		Path:         "/v1/queries/{queryId}",
		Query:        "view=full",
		Title:        "Echo",
		Description:  "Echo input",
		AllowedRoles: append([]string(nil), allowedRoles...),
		Transport:    catalog.TransportREST,
		Parameters: []catalog.CatalogParameter{
			{Name: "message", Type: "string", Description: "message", Required: true},
			{Name: "query_id", WireName: "queryId", Type: "string", Location: "path", Description: "query id", Required: true},
		},
	}
}

func roundTripPluginOperation(allowedRoles ...string) catalog.CatalogOperation {
	return catalog.CatalogOperation{
		ID:           "plugin_echo",
		Method:       http.MethodPost,
		Description:  "Plugin echo",
		AllowedRoles: append([]string(nil), allowedRoles...),
		Parameters: []catalog.CatalogParameter{
			{Name: "message", Type: "string", Description: "message", Required: true},
		},
	}
}

func roundTripStaticCatalog() *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:        "roundtrip",
		DisplayName: "Round Trip",
		Description: "test provider",
		BaseURL:     "https://roundtrip.example.com",
		AuthStyle:   "bearer",
		Headers: map[string]string{
			"X-Env": "test",
		},
		Operations: []catalog.CatalogOperation{
			roundTripOperation("admin"),
			roundTripPluginOperation("admin"),
		},
	}
	coreintegration.CompileSchemas(cat)
	return cat
}

func (p *roundTripProvider) Catalog() *catalog.Catalog {
	return roundTripStaticCatalog()
}

func (p *roundTripProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	subjectID := ""
	subjectKind := ""
	authSource := ""
	displayName := ""
	identityPresent := "false"
	if p := principal.FromContext(ctx); p != nil {
		subjectID = p.SubjectID
		if subjectID == "" && p.UserID != "" {
			subjectID = principal.UserSubjectID(p.UserID)
		}
		subjectKind = string(p.Kind)
		authSource = p.AuthSource()
		displayName = p.DisplayName
		if p.Identity != nil {
			identityPresent = "true"
		}
	}
	credential := invocation.CredentialContextFromContext(ctx)
	access := invocation.AccessContextFromContext(ctx)
	return &catalog.Catalog{
		Name:        "roundtrip-session",
		DisplayName: fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s", token, subjectID, subjectKind, displayName, identityPresent, authSource, credential.Mode, access.Policy, access.Role),
		Description: "session catalog",
		Operations: []catalog.CatalogOperation{
			{
				ID:           "echo",
				AllowedRoles: []string{"viewer"},
				Parameters: []catalog.CatalogParameter{
					{Name: "message", Type: "string", Description: "message", Required: true},
					{Name: "query_id", Type: "string", Description: "query id", Required: true},
				},
			},
			{
				ID:           "plugin_echo",
				AllowedRoles: []string{"viewer"},
				Parameters: []catalog.CatalogParameter{
					{Name: "message", Type: "string", Description: "message", Required: true},
				},
			},
		},
	}, nil
}

type manualOnlySDKProvider struct{}

func (p *manualOnlySDKProvider) Configure(_ context.Context, _ string, _ map[string]any) error {
	return nil
}

func (p *manualOnlySDKProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*sdkgestalt.OperationResult, error) {
	return &sdkgestalt.OperationResult{Status: 200, Body: `{}`}, nil
}

func roundTripStaticSpec() StaticProviderSpec {
	return StaticProviderSpec{
		Name:           "roundtrip",
		DisplayName:    "Round Trip",
		Description:    "test provider",
		ConnectionMode: core.ConnectionModeEither,
		Catalog:        roundTripStaticCatalog(),
		AuthTypes:      []string{"manual"},
		ConnectionParams: map[string]core.ConnectionParamDef{
			"tenant":  {Required: true, Description: "Tenant slug"},
			"team_id": {From: "token_response", Field: "team_id"},
		},
	}
}

func manualOnlyStaticSpec() StaticProviderSpec {
	return StaticProviderSpec{
		Name:           "manual-only",
		DisplayName:    "Manual Only",
		Description:    "manual auth provider",
		ConnectionMode: core.ConnectionModeIdentity,
		Catalog: &catalog.Catalog{
			Name:        "manual-only",
			DisplayName: "Manual Only",
			Description: "manual auth provider",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "echo",
					Description: "Echo input",
					Method:      http.MethodPost,
					Parameters: []catalog.CatalogParameter{
						{Name: "message", Type: "string", Description: "message", Required: true, Default: "hello"},
					},
				},
			},
		},
		AuthTypes: []string{"manual"},
	}
}

func TestRemoteProviderRoundTrip(t *testing.T) {
	t.Parallel()

	client := newIntegrationProviderClient(t, NewProviderServer(&roundTripProvider{}))
	prov, err := NewRemoteProvider(context.Background(), client, roundTripStaticSpec(), nil)
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

	if _, ok := prov.(core.SessionCatalogProvider); !ok {
		t.Fatal("expected remote provider to implement SessionCatalogProvider")
	}
	if got := prov.AuthTypes(); len(got) != 1 || got[0] != "manual" {
		t.Fatalf("unexpected auth types: %#v", got)
	}
	if cat := prov.Catalog(); cat == nil || len(cat.Operations) != 2 || cat.Operations[0].ID != "echo" {
		t.Fatalf("unexpected Catalog result: %+v", cat)
	}

	cases := []struct {
		name               string
		principal          *principal.Principal
		wantExecuteBody    string
		wantSessionCatalog string
	}{
		{
			name: "user subject",
			principal: &principal.Principal{
				UserID:      "user-123",
				SubjectID:   principal.UserSubjectID("user-123"),
				DisplayName: "Ada",
				Kind:        principal.KindUser,
				Identity:    &core.UserIdentity{DisplayName: "Ada"},
				Source:      principal.SourceAPIToken,
			},
			wantExecuteBody:    "echo|secret-token|hi|acme|user:user-123|user|Ada|true|api_token|identity|identity:__identity__|roadmap|admin",
			wantSessionCatalog: "token-123|user:user-123|user|Ada|true|api_token|identity|roadmap|admin",
		},
		{
			name: "workload subject",
			principal: &principal.Principal{
				SubjectID:   principal.WorkloadSubjectID("triage-bot"),
				DisplayName: "Triage Bot",
				Kind:        principal.KindWorkload,
				Source:      principal.SourceWorkloadToken,
			},
			wantExecuteBody:    "echo|secret-token|hi|acme|workload:triage-bot|workload|Triage Bot|false|workload_token|identity|identity:__identity__|roadmap|admin",
			wantSessionCatalog: "token-123|workload:triage-bot|workload|Triage Bot|false|workload_token|identity|roadmap|admin",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := core.WithConnectionParams(context.Background(), map[string]string{"tenant": "acme"})
			ctx = principal.WithPrincipal(ctx, tc.principal)
			ctx = invocation.WithCredentialContext(ctx, invocation.CredentialContext{
				Mode:       core.ConnectionModeIdentity,
				SubjectID:  "identity:__identity__",
				Connection: "workspace",
				Instance:   "default",
			})
			ctx = invocation.WithAccessContext(ctx, invocation.AccessContext{
				Policy: "roadmap",
				Role:   "admin",
			})

			result, err := prov.Execute(ctx, "echo", map[string]any{"message": "hi"}, "secret-token")
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if result.Status != 201 || result.Body != tc.wantExecuteBody {
				t.Fatalf("unexpected execute result: %+v", result)
			}

			if cat := prov.Catalog(); cat == nil || cat.Name != "roundtrip" {
				t.Fatalf("unexpected static catalog: %+v", cat)
			} else if got := cat.Operations[0].AllowedRoles; len(got) != 1 || got[0] != "admin" {
				t.Fatalf("unexpected static catalog allowedRoles: %#v", got)
			}

			scp := prov.(core.SessionCatalogProvider)
			sessionCat, err := scp.CatalogForRequest(ctx, "token-123")
			if err != nil {
				t.Fatalf("CatalogForRequest: %v", err)
			}
			if sessionCat.Name != "roundtrip-session" || sessionCat.DisplayName != tc.wantSessionCatalog {
				t.Fatalf("unexpected session catalog: %+v", sessionCat)
			}
			if sessionCat.BaseURL != "https://roundtrip.example.com" || sessionCat.AuthStyle != "bearer" {
				t.Fatalf("unexpected session catalog transport metadata: %+v", sessionCat)
			}
			if got := sessionCat.Headers["X-Env"]; got != "test" {
				t.Fatalf("unexpected session catalog headers: %+v", sessionCat.Headers)
			}
			if len(sessionCat.Operations) != 2 {
				t.Fatalf("unexpected session catalog operations: %+v", sessionCat.Operations)
			}
			opsByID := map[string]catalog.CatalogOperation{}
			for _, op := range sessionCat.Operations {
				opsByID[op.ID] = op
			}
			echoOp := opsByID["echo"]
			if got := echoOp.AllowedRoles; len(got) != 1 || got[0] != "viewer" {
				t.Fatalf("unexpected session catalog allowedRoles: %#v", got)
			}
			if echoOp.Transport != catalog.TransportREST {
				t.Fatalf("unexpected session catalog operation identifiers: %+v", echoOp)
			}
			if echoOp.Path != "/v1/queries/{queryId}" || echoOp.Query != "view=full" {
				t.Fatalf("unexpected session catalog request metadata: %+v", echoOp)
			}
			if len(echoOp.InputSchema) == 0 {
				t.Fatalf("expected synthesized input schema, got %+v", echoOp)
			}
			if len(echoOp.Parameters) != 2 {
				t.Fatalf("unexpected session catalog parameters: %+v", echoOp.Parameters)
			}
			if echoOp.Parameters[1].WireName != "queryId" || echoOp.Parameters[1].Location != "path" {
				t.Fatalf("unexpected session catalog path parameter metadata: %+v", echoOp.Parameters[1])
			}
			if pluginOp := opsByID["plugin_echo"]; pluginOp.Transport != catalog.TransportPlugin {
				t.Fatalf("expected plugin session operation to preserve plugin transport, got %+v", pluginOp)
			}
		})
	}

	if defs := prov.ConnectionParamDefs(); defs["tenant"].Description != "Tenant slug" || defs["team_id"].Field != "team_id" {
		t.Fatalf("unexpected connection param defs: %+v", defs)
	}
}

func TestRequestContextProto_PreservesWorkloadDisplayName(t *testing.T) {
	t.Parallel()

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID:   principal.WorkloadSubjectID("triage-bot"),
		DisplayName: "Triage Bot",
		Kind:        principal.KindWorkload,
		Source:      principal.SourceWorkloadToken,
	})
	ctx = invocation.WithAccessContext(ctx, invocation.AccessContext{
		Policy: "roadmap",
		Role:   "viewer",
	})

	reqCtx := requestContextProto(ctx)
	if reqCtx == nil || reqCtx.GetSubject() == nil {
		t.Fatal("expected request subject context")
	}
	if got := reqCtx.GetSubject().GetDisplayName(); got != "Triage Bot" {
		t.Fatalf("subject display name = %q, want %q", got, "Triage Bot")
	}
	if reqCtx.GetAccess() == nil || reqCtx.GetAccess().GetPolicy() != "roadmap" || reqCtx.GetAccess().GetRole() != "viewer" {
		t.Fatalf("unexpected access context: %#v", reqCtx.GetAccess())
	}
}

func TestPrincipalFromProto_WorkloadDisplayNameDoesNotCreateIdentity(t *testing.T) {
	t.Parallel()

	p := principalFromProto(&proto.SubjectContext{
		Id:          principal.WorkloadSubjectID("triage-bot"),
		Kind:        string(principal.KindWorkload),
		DisplayName: "Triage Bot",
		AuthSource:  principal.SourceWorkloadToken.String(),
	})
	if p == nil {
		t.Fatal("expected principal")
	}
	if p.DisplayName != "Triage Bot" {
		t.Fatalf("display name = %q, want %q", p.DisplayName, "Triage Bot")
	}
	if p.Identity != nil {
		t.Fatalf("expected workload identity to remain nil, got %#v", p.Identity)
	}
}

func TestRemoteProviderManualAuthOnly(t *testing.T) {
	t.Parallel()

	client := newIntegrationProviderClient(t, sdkgestalt.NewProviderServer(&manualOnlySDKProvider{}, (*sdkgestalt.Router[manualOnlySDKProvider])(nil)))
	prov, err := NewRemoteProvider(context.Background(), client, manualOnlyStaticSpec(), nil)
	if err != nil {
		t.Fatalf("NewRemoteProvider: %v", err)
	}

	if got := prov.AuthTypes(); len(got) != 1 || got[0] != "manual" {
		t.Fatalf("AuthTypes = %#v, want [manual]", got)
	}
	cat := prov.Catalog()
	if cat == nil || len(cat.Operations) != 1 {
		t.Fatalf("unexpected catalog: %+v", cat)
	}
	if cat.Operations[0].Transport != catalog.TransportPlugin {
		t.Fatalf("Transport = %q, want %q", cat.Operations[0].Transport, catalog.TransportPlugin)
	}
}

func TestDeclarativeProviderUnknownOperationReturnsNotFound(t *testing.T) {
	t.Parallel()

	prov, err := NewDeclarativeProvider(&providermanifestv1.Manifest{
		Source:      "declarative",
		DisplayName: "Declarative",
		Description: "REST manifest provider",
		Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://api.example.com",
					Operations: []providermanifestv1.ProviderOperation{
						{Name: "echo", Method: http.MethodGet, Path: "/echo"},
					},
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}

	result, err := prov.Execute(context.Background(), "missing", nil, "")
	if err != nil {
		t.Fatalf("Execute(missing): %v", err)
	}
	if result.Status != http.StatusNotFound || result.Body != `{"error":"unknown operation"}` {
		t.Fatalf("missing operation result = %+v", result)
	}
}
