package plugins

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	sdkgestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
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
	host := invocation.HostContextFromContext(ctx)
	idempotencyKey := invocation.IdempotencyKeyFromContext(ctx)
	return &core.OperationResult{
		Status: 201,
		Body:   fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s", operation, token, params["message"], core.ConnectionParams(ctx)["tenant"], subjectID, subjectKind, displayName, identityPresent, authSource, credential.Mode, credential.SubjectID, access.Policy, access.Role, idempotencyKey, host.PublicBaseURL),
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
	host := invocation.HostContextFromContext(ctx)
	return &catalog.Catalog{
		Name:        "roundtrip-session",
		DisplayName: fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s", token, subjectID, subjectKind, displayName, identityPresent, authSource, credential.Mode, access.Policy, access.Role, host.PublicBaseURL),
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

func (p *roundTripProvider) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	return &core.HTTPResolvedSubject{
		ID:          "user:resolved",
		Kind:        "user",
		DisplayName: invocation.HostContextFromContext(ctx).PublicBaseURL,
		AuthSource:  req.VerifiedSubject,
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
	prov, err := NewRemoteProvider(context.Background(), client, roundTripStaticSpec(), nil, WithHostContext(" https://gestalt.example.test/ "))
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
	if !core.SupportsSessionCatalog(prov) {
		t.Fatal("expected remote provider to support session catalogs")
	}
	if _, ok := prov.(core.PostConnectCapable); !ok {
		t.Fatal("expected remote provider to implement PostConnectCapable")
	}
	if !core.SupportsPostConnect(prov) {
		t.Fatal("expected remote provider to support post-connect")
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
			wantExecuteBody:    "echo|secret-token|hi|acme|user:user-123|user|Ada|true|api_token|user|user:user-123|roadmap|admin|tool-call-123|https://gestalt.example.test",
			wantSessionCatalog: "token-123|user:user-123|user|Ada|true|api_token|user|roadmap|admin|https://gestalt.example.test",
		},
		{
			name: "service account subject",
			principal: &principal.Principal{
				SubjectID:   "service_account:triage-bot",
				DisplayName: "Triage Bot",
				Kind:        principal.Kind("service_account"),
				Source:      principal.SourceAPIToken,
			},
			wantExecuteBody:    "echo|secret-token|hi|acme|service_account:triage-bot|service_account|Triage Bot|false|api_token|user|service_account:triage-bot|roadmap|admin|tool-call-123|https://gestalt.example.test",
			wantSessionCatalog: "token-123|service_account:triage-bot|service_account|Triage Bot|false|api_token|user|roadmap|admin|https://gestalt.example.test",
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
			ctx = invocation.WithIdempotencyKey(ctx, " tool-call-123 ")

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

			sessionCat, attempted, err := core.CatalogForRequest(ctx, prov, "token-123")
			if err != nil {
				t.Fatalf("CatalogForRequest: %v", err)
			}
			if !attempted {
				t.Fatal("expected core.CatalogForRequest to report attempted")
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

			metadata, supported, err := core.PostConnect(ctx, prov, &core.ExternalCredential{
				SubjectID:  principal.EffectiveCredentialSubjectID(tc.principal),
				Connection: "workspace",
				Instance:   "default",
			})
			if err != nil {
				t.Fatalf("PostConnect: %v", err)
			}
			if !supported {
				t.Fatal("expected core.PostConnect to report support")
			}
			if !reflect.DeepEqual(metadata, map[string]string{
				"subject":    principal.EffectiveCredentialSubjectID(tc.principal),
				"connection": "workspace",
				"instance":   "default",
			}) {
				t.Fatalf("unexpected post connect metadata: %+v", metadata)
			}

			resolved, attempted, err := core.ResolveHTTPSubject(context.Background(), prov, &core.HTTPSubjectResolveRequest{
				VerifiedSubject: "host-only",
			})
			if err != nil {
				t.Fatalf("ResolveHTTPSubject: %v", err)
			}
			if !attempted {
				t.Fatal("expected core.ResolveHTTPSubject to report attempted")
			}
			if resolved == nil || resolved.DisplayName != "https://gestalt.example.test" || resolved.AuthSource != "host-only" {
				t.Fatalf("unexpected resolved subject: %+v", resolved)
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

	reqCtx, err := requestContextProto(ctx, "")
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

func TestRequestContextProto_IncludesRunAsAgentSubject(t *testing.T) {
	t.Parallel()

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "service_account:github_app_installation:99:repo:acme/widgets",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	})
	ctx = invocation.WithRunAsAudit(ctx, &core.RunAsSubject{
		SubjectID:           "user:user-123",
		SubjectKind:         "user",
		CredentialSubjectID: "user:user-123",
		DisplayName:         "Ada Lovelace",
		AuthSource:          "slack",
	}, &core.RunAsSubject{
		SubjectID:   "service_account:github_app_installation:99:repo:acme/widgets",
		SubjectKind: "service_account",
		AuthSource:  "github_app_webhook",
	})

	reqCtx, err := requestContextProto(ctx, "")
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
	if reqCtx == nil || reqCtx.GetAgentSubject() == nil {
		t.Fatal("expected agent subject context")
	}
	if got := reqCtx.GetAgentSubject().GetDisplayName(); got != "Ada Lovelace" {
		t.Fatalf("agent subject display name = %q, want Ada Lovelace", got)
	}
}

func TestRequestContextProto_IncludesAgentExternalIdentity(t *testing.T) {
	t.Parallel()

	ctx := invocation.WithAgentExternalIdentityContext(context.Background(), invocation.ExternalIdentityContext{
		Type: "github_identity",
		ID:   "user:12345678",
	})
	reqCtx, err := requestContextProto(ctx, "")
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
	if reqCtx == nil || reqCtx.GetAgentExternalIdentity() == nil {
		t.Fatal("expected agent external identity context")
	}
	if got := reqCtx.GetAgentExternalIdentity().GetType(); got != "github_identity" {
		t.Fatalf("agent external identity type = %q, want github_identity", got)
	}
	if got := reqCtx.GetAgentExternalIdentity().GetId(); got != "user:12345678" {
		t.Fatalf("agent external identity id = %q, want user:12345678", got)
	}
}

func TestRequestContextProto_IncludesInvocationExternalIdentity(t *testing.T) {
	t.Parallel()

	ctx := invocation.WithExternalIdentityContext(context.Background(), invocation.ExternalIdentityContext{
		Type: "github_app_installation",
		ID:   "repo:acme/widgets",
	})
	reqCtx, err := requestContextProto(ctx, "")
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
	if reqCtx == nil || reqCtx.GetExternalIdentity() == nil {
		t.Fatal("expected external identity context")
	}
	if got := reqCtx.GetExternalIdentity().GetType(); got != "github_app_installation" {
		t.Fatalf("external identity type = %q, want github_app_installation", got)
	}
	if got := reqCtx.GetExternalIdentity().GetId(); got != "repo:acme/widgets" {
		t.Fatalf("external identity id = %q, want repo:acme/widgets", got)
	}
}

func TestApplyRequestContext_IncludesDelegatedExternalIdentities(t *testing.T) {
	t.Parallel()

	ctx := applyRequestContext(context.Background(), &proto.RequestContext{
		Subject: &proto.SubjectContext{
			Id:          "service_account:github-toolshed",
			Kind:        "service_account",
			DisplayName: "GitHub Toolshed",
			AuthSource:  "managed_subject",
		},
		AgentSubject: &proto.SubjectContext{
			Id:          "user:user-123",
			Kind:        "user",
			DisplayName: "Ada Lovelace",
			AuthSource:  "slack",
		},
		ExternalIdentity: &proto.ExternalIdentityContext{
			Type: "github_app_installation",
			Id:   "repo:acme/widgets",
		},
		AgentExternalIdentity: &proto.ExternalIdentityContext{
			Type: "github_identity",
			Id:   "user:12345678",
		},
	})

	if got := invocation.ExternalIdentityContextFromContext(ctx); got.Type != "github_app_installation" || got.ID != "repo:acme/widgets" {
		t.Fatalf("external identity context = %#v", got)
	}
	if got := invocation.AgentExternalIdentityContextFromContext(ctx); got.Type != "github_identity" || got.ID != "user:12345678" {
		t.Fatalf("agent external identity context = %#v", got)
	}
	audit := invocation.RunAsAuditFromContext(ctx)
	if audit.AgentSubject == nil || audit.AgentSubject.SubjectID != "user:user-123" {
		t.Fatalf("agent subject audit = %#v", audit.AgentSubject)
	}
	if audit.RunAsSubject == nil || audit.RunAsSubject.SubjectID != "service_account:github-toolshed" {
		t.Fatalf("runAs subject audit = %#v", audit.RunAsSubject)
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

	reqCtx, err := requestContextProto(ctx, "")
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

func TestRequestContextProto_PreservesHostOnlyContext(t *testing.T) {
	t.Parallel()

	reqCtx, err := requestContextProto(context.Background(), " https://valon.tools/ ")
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
	if reqCtx == nil || reqCtx.GetHost() == nil {
		t.Fatal("expected host request context")
	}
	if got := reqCtx.GetHost().GetPublicBaseUrl(); got != "https://valon.tools" {
		t.Fatalf("public base URL = %q, want %q", got, "https://valon.tools")
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
		Id:          "service_account:github-app-installation-127579767",
		Kind:        "service_account",
		DisplayName: "GitHub App installation 127579767",
		AuthSource:  "github_app_webhook",
	})
	if p == nil {
		t.Fatal("expected principal")
	}
	if p.AuthSource() != "github_app_webhook" {
		t.Fatalf("auth source = %q, want github_app_webhook", p.AuthSource())
	}
	if p.Kind != principal.Kind("service_account") {
		t.Fatalf("kind = %q, want service_account", p.Kind)
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

type unsupportedCapabilityProviderServer struct {
	proto.UnimplementedIntegrationProviderServer
	metadataErr         error
	sessionCatalogCalls atomic.Int32
	postConnectCalls    atomic.Int32
}

func (s *unsupportedCapabilityProviderServer) GetMetadata(context.Context, *emptypb.Empty) (*proto.ProviderMetadata, error) {
	if s.metadataErr != nil {
		return nil, s.metadataErr
	}
	return &proto.ProviderMetadata{}, nil
}

func (s *unsupportedCapabilityProviderServer) StartProvider(context.Context, *proto.StartProviderRequest) (*proto.StartProviderResponse, error) {
	return &proto.StartProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *unsupportedCapabilityProviderServer) Execute(context.Context, *proto.ExecuteRequest) (*proto.OperationResult, error) {
	return &proto.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
}

func (s *unsupportedCapabilityProviderServer) GetSessionCatalog(context.Context, *proto.GetSessionCatalogRequest) (*proto.GetSessionCatalogResponse, error) {
	s.sessionCatalogCalls.Add(1)
	return nil, status.Error(codes.Unimplemented, "session catalog is not implemented")
}

func (s *unsupportedCapabilityProviderServer) PostConnect(context.Context, *proto.PostConnectRequest) (*proto.PostConnectResponse, error) {
	s.postConnectCalls.Add(1)
	return nil, status.Error(codes.Unimplemented, "post connect is not implemented")
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

func TestRemoteProviderUnsupportedCapabilitiesDoNotDispatchRPCs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		metadataErr error
	}{
		{name: "metadata false"},
		{name: "metadata unimplemented", metadataErr: status.Error(codes.Unimplemented, "metadata is not implemented")},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := &unsupportedCapabilityProviderServer{metadataErr: tc.metadataErr}
			prov, err := NewRemoteProvider(context.Background(), newIntegrationProviderClient(t, server), manualOnlyStaticSpec(), nil)
			if err != nil {
				t.Fatalf("NewRemoteProvider: %v", err)
			}
			if _, ok := prov.(core.SessionCatalogProvider); !ok {
				t.Fatal("expected remote provider to implement SessionCatalogProvider")
			}
			if _, ok := prov.(core.PostConnectCapable); !ok {
				t.Fatal("expected remote provider to implement PostConnectCapable")
			}

			if core.SupportsSessionCatalog(prov) {
				t.Fatal("expected remote provider to report no session catalog support")
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

			scp := prov.(core.SessionCatalogProvider)
			_, err = scp.CatalogForRequest(context.Background(), "tok")
			if !errors.Is(err, core.ErrSessionCatalogUnsupported) {
				t.Fatalf("direct CatalogForRequest error = %v, want ErrSessionCatalogUnsupported", err)
			}

			if core.SupportsPostConnect(prov) {
				t.Fatal("expected remote provider to report no post-connect support")
			}
			metadata, supported, err := core.PostConnect(context.Background(), prov, &core.ExternalCredential{})
			if err != nil {
				t.Fatalf("PostConnect: %v", err)
			}
			if supported {
				t.Fatal("expected core.PostConnect to report unsupported")
			}
			if metadata != nil {
				t.Fatalf("PostConnect metadata = %#v, want nil", metadata)
			}

			pcp := prov.(core.PostConnectCapable)
			_, err = pcp.PostConnect(context.Background(), &core.ExternalCredential{})
			if !errors.Is(err, core.ErrPostConnectUnsupported) {
				t.Fatalf("direct PostConnect error = %v, want ErrPostConnectUnsupported", err)
			}

			if got := server.sessionCatalogCalls.Load(); got != 0 {
				t.Fatalf("GetSessionCatalog calls = %d, want 0", got)
			}
			if got := server.postConnectCalls.Load(); got != 0 {
				t.Fatalf("PostConnect calls = %d, want 0", got)
			}
		})
	}
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
