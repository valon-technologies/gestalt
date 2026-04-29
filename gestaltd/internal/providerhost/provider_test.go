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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
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
		subjectID = principal.Canonicalized(p).SubjectID
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
		subjectID = principal.Canonicalized(p).SubjectID
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
			{ID: "echo", Method: http.MethodPost, AllowedRoles: []string{"viewer"}, Tags: []string{"roundtrip", "session"}},
		},
	}, nil
}

func (p *roundTripProvider) PostConnect(_ context.Context, token *core.ExternalCredential) (map[string]string, error) {
	if token == nil {
		return nil, fmt.Errorf("token is required")
	}
	return map[string]string{
		"subject":    token.SubjectID,
		"connection": token.Connection,
		"instance":   token.Instance,
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
		Description:    "manual authentication provider",
		ConnectionMode: core.ConnectionModeUser,
		Catalog: &catalog.Catalog{
			Name:        "manual-only",
			DisplayName: "Manual Only",
			Description: "manual authentication provider",
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
	if _, ok := prov.(core.PostConnectCapable); !ok {
		t.Fatal("expected remote provider to implement PostConnectCapable")
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
			wantExecuteBody:    "echo|secret-token|hi|acme|user:user-123|user|Ada|true|api_token|user|user:user-123|roadmap|admin",
			wantSessionCatalog: "token-123|user:user-123|user|Ada|true|api_token|user|roadmap|admin",
		},
		{
			name: "service account subject",
			principal: &principal.Principal{
				SubjectID:   "service_account:triage-bot",
				DisplayName: "Triage Bot",
				Kind:        principal.Kind("service_account"),
				Source:      principal.SourceAPIToken,
			},
			wantExecuteBody:    "echo|secret-token|hi|acme|service_account:triage-bot|service_account|Triage Bot|false|api_token|user|service_account:triage-bot|roadmap|admin",
			wantSessionCatalog: "token-123|service_account:triage-bot|service_account|Triage Bot|false|api_token|user|roadmap|admin",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := core.WithConnectionParams(context.Background(), map[string]string{"tenant": "acme"})
			ctx = principal.WithPrincipal(ctx, tc.principal)
			ctx = invocation.WithCredentialContext(ctx, invocation.CredentialContext{
				Mode:       core.ConnectionModeUser,
				SubjectID:  principal.EffectiveCredentialSubjectID(tc.principal),
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
			if got := sessionCat.Operations[0].Tags; len(got) != 2 || got[0] != "roundtrip" || got[1] != "session" {
				t.Fatalf("unexpected session catalog tags: %#v", got)
			}

			pcp := prov.(core.PostConnectCapable)
			metadata, err := pcp.PostConnect(ctx, &core.ExternalCredential{
				SubjectID:  principal.EffectiveCredentialSubjectID(tc.principal),
				Connection: "workspace",
				Instance:   "default",
			})
			if err != nil {
				t.Fatalf("PostConnect: %v", err)
			}
			if !reflect.DeepEqual(metadata, map[string]string{
				"subject":    principal.EffectiveCredentialSubjectID(tc.principal),
				"connection": "workspace",
				"instance":   "default",
			}) {
				t.Fatalf("unexpected post connect metadata: %+v", metadata)
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

func TestRequestContextProto_PreservesServiceAccountDisplayName(t *testing.T) {
	t.Parallel()

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID:   "service_account:triage-bot",
		DisplayName: "Triage Bot",
		Kind:        principal.Kind("service_account"),
		Source:      principal.SourceAPIToken,
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

func TestPrincipalFromProto_NonUserDisplayNameDoesNotCreateIdentity(t *testing.T) {
	t.Parallel()

	p := principalFromProto(&proto.SubjectContext{
		Id:          "service_account:triage-bot",
		Kind:        "service_account",
		DisplayName: "Triage Bot",
		AuthSource:  principal.SourceAPIToken.String(),
	})
	if p == nil {
		t.Fatal("expected principal")
	}
	if p.DisplayName != "Triage Bot" {
		t.Fatalf("display name = %q, want %q", p.DisplayName, "Triage Bot")
	}
	if p.Kind != principal.Kind("service_account") {
		t.Fatalf("kind = %q, want service_account", p.Kind)
	}
	if p.Identity != nil {
		t.Fatalf("expected non-user identity to remain nil, got %#v", p.Identity)
	}
}

func TestPrincipalFromProto_PreservesCustomAuthSource(t *testing.T) {
	t.Parallel()

	p := principalFromProto(&proto.SubjectContext{
		Id:          "workload:github_app_installation:127579767:repo:valon-technologies/gestalt",
		Kind:        "workload",
		DisplayName: "GitHub App installation 127579767",
		AuthSource:  "github_app_webhook",
	})
	if p == nil {
		t.Fatal("expected principal")
	}
	if p.AuthSource() != "github_app_webhook" {
		t.Fatalf("auth source = %q, want github_app_webhook", p.AuthSource())
	}
	if p.Kind != principal.Kind("workload") {
		t.Fatalf("kind = %q, want workload", p.Kind)
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

type metadataFailureProviderServer struct {
	proto.UnimplementedIntegrationProviderServer
}

func (s *metadataFailureProviderServer) GetMetadata(context.Context, *emptypb.Empty) (*proto.ProviderMetadata, error) {
	return nil, status.Error(codes.Unknown, "provider metadata: metadata exploded")
}

type unavailableMetadataProviderServer struct {
	cancel context.CancelFunc
}

func (c *unavailableMetadataProviderServer) GetMetadata(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.ProviderMetadata, error) {
	if c.cancel != nil {
		c.cancel()
	}
	return nil, status.Error(codes.Unavailable, "metadata warming up")
}

func (*unavailableMetadataProviderServer) StartProvider(context.Context, *proto.StartProviderRequest, ...grpc.CallOption) (*proto.StartProviderResponse, error) {
	panic("unexpected StartProvider call")
}

func (*unavailableMetadataProviderServer) Execute(context.Context, *proto.ExecuteRequest, ...grpc.CallOption) (*proto.OperationResult, error) {
	panic("unexpected Execute call")
}

func (*unavailableMetadataProviderServer) ResolveHTTPSubject(context.Context, *proto.ResolveHTTPSubjectRequest, ...grpc.CallOption) (*proto.ResolveHTTPSubjectResponse, error) {
	panic("unexpected ResolveHTTPSubject call")
}

func (*unavailableMetadataProviderServer) GetSessionCatalog(context.Context, *proto.GetSessionCatalogRequest, ...grpc.CallOption) (*proto.GetSessionCatalogResponse, error) {
	panic("unexpected GetSessionCatalog call")
}

func (*unavailableMetadataProviderServer) PostConnect(context.Context, *proto.PostConnectRequest, ...grpc.CallOption) (*proto.PostConnectResponse, error) {
	panic("unexpected PostConnect call")
}

func TestNewRemoteProviderLabelsMetadataFailures(t *testing.T) {
	t.Parallel()

	client := newIntegrationProviderClient(t, &metadataFailureProviderServer{})
	_, err := NewRemoteProvider(context.Background(), client, roundTripStaticSpec(), nil)
	if err == nil {
		t.Fatal("expected NewRemoteProvider to fail")
	}
	if got := err.Error(); got != `get provider metadata: rpc error: code = Unknown desc = provider metadata: metadata exploded` {
		t.Fatalf("NewRemoteProvider error = %q", got)
	}
}

func TestGetIntegrationProviderSupportWithRetryLabelsContextDoneFailures(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	_, err := getIntegrationProviderSupportWithRetry(ctx, &unavailableMetadataProviderServer{cancel: cancel})
	if err == nil {
		t.Fatal("expected getIntegrationProviderSupportWithRetry to fail")
	}
	if got := err.Error(); got != `get provider metadata: rpc error: code = Unavailable desc = metadata warming up` {
		t.Fatalf("getIntegrationProviderSupportWithRetry error = %q", got)
	}
}
