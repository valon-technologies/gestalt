package providerhost

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	sdkgestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type roundTripProvider struct{}

func (p *roundTripProvider) Configure(_ context.Context, _ string, _ map[string]any) error {
	return nil
}
func (p *roundTripProvider) Name() string                        { return "roundtrip" }
func (p *roundTripProvider) DisplayName() string                 { return "Round Trip" }
func (p *roundTripProvider) Description() string                 { return "test provider" }
func (p *roundTripProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }
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

func (p *roundTripProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name:        "roundtrip",
		DisplayName: "Round Trip",
		Description: "test provider",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost, AllowedRoles: []string{"admin"}},
		},
	}
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
			{ID: "echo", Method: http.MethodPost, AllowedRoles: []string{"viewer"}},
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
		ConnectionMode: core.ConnectionModeUser,
		Catalog: &catalog.Catalog{
			Name:        "roundtrip",
			DisplayName: "Round Trip",
			Description: "test provider",
			Operations: []catalog.CatalogOperation{
				{ID: "echo", Method: http.MethodPost, AllowedRoles: []string{"admin"}},
			},
		},
		AuthTypes: []string{"manual"},
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
	if prov.ConnectionMode() != core.ConnectionModeUser {
		t.Fatalf("unexpected connection mode: %q", prov.ConnectionMode())
	}

	if _, ok := prov.(core.SessionCatalogProvider); !ok {
		t.Fatal("expected remote provider to implement SessionCatalogProvider")
	}
	if got := prov.AuthTypes(); len(got) != 1 || got[0] != "manual" {
		t.Fatalf("unexpected auth types: %#v", got)
	}
	if cat := prov.Catalog(); cat == nil || len(cat.Operations) != 1 || cat.Operations[0].ID != "echo" {
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
			name: "identity token subject",
			principal: &principal.Principal{
				SubjectID:   principal.IdentitySubjectID("triage-bot"),
				DisplayName: "Triage Bot",
				Kind:        principal.KindIdentity,
				Source:      principal.SourceIdentityToken,
			},
			wantExecuteBody:    "echo|secret-token|hi|acme|identity:triage-bot|identity|Triage Bot|false|identity_token|identity|identity:__identity__|roadmap|admin",
			wantSessionCatalog: "token-123|identity:triage-bot|identity|Triage Bot|false|identity_token|identity|roadmap|admin",
		},
		{
			name: "managed identity subject",
			principal: &principal.Principal{
				SubjectID:   principal.ManagedIdentitySubjectID("release-bot"),
				DisplayName: "Release Bot",
				Kind:        principal.KindIdentity,
				Source:      principal.SourceAPIToken,
			},
			wantExecuteBody:    "echo|secret-token|hi|acme|identity:release-bot|identity|Release Bot|false|api_token|identity|identity:__identity__|roadmap|admin",
			wantSessionCatalog: "token-123|identity:release-bot|identity|Release Bot|false|api_token|identity|roadmap|admin",
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
			if got := sessionCat.Operations[0].AllowedRoles; len(got) != 1 || got[0] != "viewer" {
				t.Fatalf("unexpected session catalog allowedRoles: %#v", got)
			}
		})
	}

	t.Run("invalid workflow context", func(t *testing.T) {
		t.Parallel()

		ctx := invocation.WithWorkflowContext(context.Background(), map[string]any{
			"input": map[string]any{
				"bad": make(chan int),
			},
		})

		if _, err := prov.Execute(ctx, "echo", map[string]any{"message": "hi"}, "secret-token"); err == nil {
			t.Fatal("expected Execute to fail for unserializable workflow context")
		}
	})

	if defs := prov.ConnectionParamDefs(); defs["tenant"].Description != "Tenant slug" || defs["team_id"].Field != "team_id" {
		t.Fatalf("unexpected connection param defs: %+v", defs)
	}
}

func TestRequestContextProto_PreservesWorkloadDisplayName(t *testing.T) {
	t.Parallel()

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID:   principal.IdentitySubjectID("triage-bot"),
		DisplayName: "Triage Bot",
		Kind:        principal.KindIdentity,
		Source:      principal.SourceIdentityToken,
	})
	ctx = invocation.WithAccessContext(ctx, invocation.AccessContext{
		Policy: "roadmap",
		Role:   "viewer",
	})

	reqCtx, err := requestContextProto(ctx)
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
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

func TestRequestContextProto_PreservesWorkflowContext(t *testing.T) {
	t.Parallel()

	ctx := invocation.WithWorkflowContext(context.Background(), map[string]any{
		"runId": "run-123",
		"trigger": map[string]any{
			"kind": "event",
		},
	})

	reqCtx, err := requestContextProto(ctx)
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
	if reqCtx == nil || reqCtx.GetWorkflow() == nil {
		t.Fatal("expected workflow request context")
	}
	if got := reqCtx.GetWorkflow().AsMap(); !reflect.DeepEqual(got, map[string]any{
		"runId": "run-123",
		"trigger": map[string]any{
			"kind": "event",
		},
	}) {
		t.Fatalf("workflow context = %#v", got)
	}
}

func TestPrincipalFromProto_WorkloadDisplayNameDoesNotCreateIdentity(t *testing.T) {
	t.Parallel()

	p := principalFromProto(&proto.SubjectContext{
		Id:          principal.IdentitySubjectID("triage-bot"),
		Kind:        string(principal.KindIdentity),
		DisplayName: "Triage Bot",
		AuthSource:  principal.SourceIdentityToken.String(),
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

func TestPrincipalFromProto_ManagedIdentityKindRoundTrips(t *testing.T) {
	t.Parallel()

	p := principalFromProto(&proto.SubjectContext{
		Id:          principal.ManagedIdentitySubjectID("release-bot"),
		Kind:        string(principal.KindIdentity),
		DisplayName: "Release Bot",
		AuthSource:  principal.SourceAPIToken.String(),
	})
	if p == nil {
		t.Fatal("expected principal")
	}
	if p.Kind != principal.KindIdentity {
		t.Fatalf("kind = %q, want %q", p.Kind, principal.KindIdentity)
	}
	if p.DisplayName != "Release Bot" {
		t.Fatalf("display name = %q, want %q", p.DisplayName, "Release Bot")
	}
	if p.Identity != nil {
		t.Fatalf("expected managed identity principal to remain non-human, got %#v", p.Identity)
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
