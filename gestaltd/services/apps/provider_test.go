package plugins

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type roundTripProvider struct{}

func (p *roundTripProvider) Configure(_ context.Context, _ string, _ map[string]any) error {
	return nil
}
func (p *roundTripProvider) Name() string                        { return "roundtrip" }
func (p *roundTripProvider) DisplayName() string                 { return "Round Trip" }
func (p *roundTripProvider) Description() string                 { return "test provider" }
func (p *roundTripProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeSubject }
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
	if operation == "missing_credential" {
		return nil, fmt.Errorf("%w: no external credential stored for integration %q", invocation.ErrNoCredential, "roundtrip")
	}
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
		Body:   []byte(fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s", operation, token, params["message"], core.ConnectionParams(ctx)["tenant"], subjectID, subjectKind, displayName, identityPresent, authSource, credential.Mode, credential.SubjectID, access.Policy, access.Role, idempotencyKey, host.PublicBaseURL)),
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

func (p *roundTripProvider) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	return &core.HTTPResolvedSubject{
		ID:          "user:resolved",
		Kind:        "user",
		DisplayName: invocation.HostContextFromContext(ctx).PublicBaseURL,
	}, nil
}

func roundTripStaticSpec() StaticProviderSpec {
	return StaticProviderSpec{
		Name:           "roundtrip",
		DisplayName:    "Round Trip",
		Description:    "test provider",
		ConnectionMode: core.ConnectionModeSubject,
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
		Description:    "manual identity provider",
		ConnectionMode: core.ConnectionModeSubject,
		Catalog: &catalog.Catalog{
			Name:        "manual-only",
			DisplayName: "Manual Only",
			Description: "manual identity provider",
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

	client := newAppProviderClient(t, NewProviderServer(&roundTripProvider{}))
	prov, err := NewRemote(context.Background(), client, roundTripStaticSpec(), nil, WithHostContext(" https://gestalt.example.test/ "))
	if err != nil {
		t.Fatalf("NewRemote: %v", err)
	}

	if prov.Name() != "roundtrip" {
		t.Fatalf("unexpected provider name: %q", prov.Name())
	}
	if prov.DisplayName() != "Round Trip" {
		t.Fatalf("unexpected display name: %q", prov.DisplayName())
	}
	if prov.ConnectionMode() != core.ConnectionModeSubject {
		t.Fatalf("unexpected connection mode: %q", prov.ConnectionMode())
	}

	if _, ok := prov.(core.SessionCatalogProvider); !ok {
		t.Fatal("expected remote provider to implement SessionCatalogProvider")
	}
	if !core.SupportsSessionCatalog(prov) {
		t.Fatal("expected remote provider to support session catalogs")
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
				Source:      principal.SourceBearer,
			},
			wantExecuteBody:    "echo|secret-token|hi|acme|user:user-123|user|Ada|true||subject|user:user-123|roadmap|admin|tool-call-123|https://gestalt.example.test",
			wantSessionCatalog: "token-123|user:user-123|user|Ada|true||subject|roadmap|admin|https://gestalt.example.test",
		},
		{
			name: "service account subject",
			principal: &principal.Principal{
				SubjectID:   "service_account:triage-bot",
				DisplayName: "Triage Bot",
				Kind:        principal.Kind("service_account"),
				Source:      principal.SourceBearer,
			},
			wantExecuteBody:    "echo|secret-token|hi|acme|service_account:triage-bot|service_account|Triage Bot|false||subject|service_account:triage-bot|roadmap|admin|tool-call-123|https://gestalt.example.test",
			wantSessionCatalog: "token-123|service_account:triage-bot|service_account|Triage Bot|false||subject|roadmap|admin|https://gestalt.example.test",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := core.WithConnectionParams(context.Background(), map[string]string{"tenant": "acme"})
			ctx = principal.WithPrincipal(ctx, tc.principal)
			ctx = invocation.WithCredentialContext(ctx, invocation.CredentialContext{
				Mode:       core.ConnectionModeSubject,
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
			if result.Status != 201 || string(result.Body) != tc.wantExecuteBody {
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

			resolved, attempted, err := core.ResolveHTTPSubject(context.Background(), prov, &core.HTTPSubjectResolveRequest{
				VerifiedSubject: "host-only",
			})
			if err != nil {
				t.Fatalf("ResolveHTTPSubject: %v", err)
			}
			if !attempted {
				t.Fatal("expected core.ResolveHTTPSubject to report attempted")
			}
			if resolved == nil || resolved.ID != "user:resolved" || resolved.Kind != "user" {
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

	t.Run("missing credential error", func(t *testing.T) {
		t.Parallel()

		_, err := prov.Execute(context.Background(), "missing_credential", nil, "")
		if !errors.Is(err, invocation.ErrNoCredential) {
			t.Fatalf("Execute error = %v, want ErrNoCredential", err)
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
		Source:      principal.SourceBearer,
	})
	ctx = invocation.WithAccessContext(ctx, invocation.AccessContext{
		Policy: "roadmap",
		Role:   "viewer",
	})

	reqCtx, err := requestContextProto(ctx, "", invocation.CallerProvider{})
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
	if reqCtx == nil || reqCtx.GetSubject() == nil {
		t.Fatal("expected request subject context")
	}
	if got := reqCtx.GetSubject().GetEmail(); got != "" {
		t.Fatalf("subject email = %q, want empty", got)
	}
	if got := reqCtx.GetSubject().GetDisplayName(); got != "Triage Bot" {
		t.Fatalf("subject display_name = %q, want Triage Bot", got)
	}
	if reqCtx.GetAccess() == nil || reqCtx.GetAccess().GetPolicy() != "roadmap" || reqCtx.GetAccess().GetRole() != "viewer" {
		t.Fatalf("unexpected access context: %#v", reqCtx.GetAccess())
	}
}

func TestRequestContextProto_IncludesUserEmail(t *testing.T) {
	t.Parallel()

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		UserID:    "user-123",
		SubjectID: principal.UserSubjectID("user-123"),
		Kind:      principal.KindUser,
		Identity: &core.UserIdentity{
			Email:       "ada@example.com",
			DisplayName: "Ada Lovelace",
		},
		Source: principal.SourceBearer,
	})

	reqCtx, err := requestContextProto(ctx, "", invocation.CallerProvider{})
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
	if reqCtx == nil || reqCtx.GetSubject() == nil {
		t.Fatal("expected request subject context")
	}
	if got := reqCtx.GetSubject().GetEmail(); got != "ada@example.com" {
		t.Fatalf("subject email = %q, want ada@example.com", got)
	}
	if got := reqCtx.GetSubject().GetDisplayName(); got != "Ada Lovelace" {
		t.Fatalf("subject display_name = %q, want Ada Lovelace", got)
	}
}

func TestRequestContextProto_RunAsServiceAccountDoesNotInheritUserEmail(t *testing.T) {
	t.Parallel()

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		UserID:    "user-123",
		SubjectID: principal.UserSubjectID("user-123"),
		Kind:      principal.KindUser,
		Identity: &core.UserIdentity{
			Email:       "ada@example.com",
			DisplayName: "Ada Lovelace",
		},
		Source: principal.SourceBearer,
	})
	ctx, _ = invocation.ApplyDelegation(ctx, principal.FromContext(ctx), &core.RunAsSubject{
		SubjectID: "service_account:review-bot",
	})

	reqCtx, err := requestContextProto(ctx, "", invocation.CallerProvider{})
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
	if got := reqCtx.GetSubject().GetId(); got != "service_account:review-bot" {
		t.Fatalf("subject id = %q, want service_account:review-bot", got)
	}
	if got := reqCtx.GetSubject().GetEmail(); got != "" {
		t.Fatalf("subject email = %q, want empty", got)
	}
	if got := reqCtx.GetAgentSubject().GetEmail(); got != "" {
		t.Fatalf("agent subject email = %q, want empty", got)
	}
}

func TestRequestContextProto_IncludesRunAsAgentSubject(t *testing.T) {
	t.Parallel()

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "service_account:event-handler",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceBearer,
	})
	ctx = invocation.WithRunAsAudit(ctx, &core.RunAsSubject{
		SubjectID: "user:user-123",
	}, &core.RunAsSubject{
		SubjectID: "service_account:event-handler",
	})

	reqCtx, err := requestContextProto(ctx, "", invocation.CallerProvider{})
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
	if reqCtx == nil || reqCtx.GetAgentSubject() == nil {
		t.Fatal("expected agent subject context")
	}
	if got := reqCtx.GetAgentSubject().GetEmail(); got != "" {
		t.Fatalf("agent subject email = %q, want empty", got)
	}
}

func TestApplyRequestContext_IncludesDelegatedAgentSubject(t *testing.T) {
	t.Parallel()

	ctx := applyRequestContext(context.Background(), &proto.RequestContext{
		Subject: &proto.SubjectContext{
			Id: "service_account:automation",
		},
		AgentSubject: &proto.SubjectContext{
			Id: "user:user-123",
		},
	})

	audit := invocation.RunAsAuditFromContext(ctx)
	if audit.AgentSubject == nil || audit.AgentSubject.SubjectID != "user:user-123" {
		t.Fatalf("agent subject audit = %#v", audit.AgentSubject)
	}
	if audit.RunAsSubject == nil || audit.RunAsSubject.SubjectID != "service_account:automation" {
		t.Fatalf("runAs subject audit = %#v", audit.RunAsSubject)
	}
}

func TestApplyRequestContext_PreservesUserEmail(t *testing.T) {
	t.Parallel()

	ctx := applyRequestContext(context.Background(), &proto.RequestContext{
		Subject: &proto.SubjectContext{
			Id:    "user:user-123",
			Email: "ada@example.com",
		},
	})

	p := principal.FromContext(ctx)
	if p == nil || p.Identity == nil {
		t.Fatalf("expected identity principal, got %#v", p)
	}
	if got := p.Identity.Email; got != "ada@example.com" {
		t.Fatalf("identity email = %q, want ada@example.com", got)
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

	reqCtx, err := requestContextProto(ctx, "", invocation.CallerProvider{})
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

func TestRequestContextProto_PreservesToolRefsContext(t *testing.T) {
	t.Parallel()

	ctx := invocation.WithToolRefsContext(context.Background(), []coreagent.ToolRef{{
		App:       "target",
		Operation: "reviews.get",
		RunAs: &core.RunAsSubject{
			SubjectID: "service_account:review-worker",
		},
	}})

	reqCtx, err := requestContextProto(ctx, "", invocation.CallerProvider{})
	if err != nil {
		t.Fatalf("requestContextProto: %v", err)
	}
	if !reqCtx.GetToolRefsSet() {
		t.Fatal("tool refs set = false, want true")
	}
	refs := reqCtx.GetToolRefs()
	if len(refs) != 1 {
		t.Fatalf("tool refs = %#v, want one ref", refs)
	}
	if got := refs[0].GetApp(); got != "target" {
		t.Fatalf("tool ref app = %q, want target", got)
	}
	if got := refs[0].GetOperation(); got != "reviews.get" {
		t.Fatalf("tool ref operation = %q, want reviews.get", got)
	}
	if got := refs[0].GetRunAs().GetId(); got != "service_account:review-worker" {
		t.Fatalf("tool ref runAs subject = %q, want service_account:review-worker", got)
	}
}

func TestApplyRequestContext_PreservesToolRefsContext(t *testing.T) {
	t.Parallel()

	ctx := applyRequestContext(context.Background(), &proto.RequestContext{
		ToolRefsSet: true,
		ToolRefs: []*proto.AgentToolRef{{
			App:       "target",
			Operation: "reviews.get",
			RunAs: &proto.SubjectContext{
				Id: "service_account:review-worker",
			},
		}},
	})

	refs := invocation.ToolRefsContextFromContext(ctx)
	if !refs.Set || len(refs.Refs) != 1 {
		t.Fatalf("tool refs context = %#v, want one present ref", refs)
	}
	if got := refs.Refs[0].App; got != "target" {
		t.Fatalf("tool ref app = %q, want target", got)
	}
	if got := refs.Refs[0].Operation; got != "reviews.get" {
		t.Fatalf("tool ref operation = %q, want reviews.get", got)
	}
	if refs.Refs[0].RunAs == nil || refs.Refs[0].RunAs.SubjectID != "service_account:review-worker" {
		t.Fatalf("tool ref runAs = %#v, want service_account:review-worker", refs.Refs[0].RunAs)
	}
}

func TestRequestContextProto_PreservesHostOnlyContext(t *testing.T) {
	t.Parallel()

	reqCtx, err := requestContextProto(context.Background(), " https://valon.tools/ ", invocation.CallerProvider{})
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

func TestPrincipalFromProto_NonUserEmailDoesNotCreateIdentity(t *testing.T) {
	t.Parallel()

	p := appaccessservice.PrincipalFromSubjectContext(&proto.SubjectContext{
		Id:    "service_account:triage-bot",
		Email: "spoofed@example.com",
	})
	if p == nil {
		t.Fatal("expected principal")
		return
	}
	if p.Kind != principal.Kind("service_account") {
		t.Fatalf("kind = %q, want service_account", p.Kind)
	}
	if p.Identity != nil {
		t.Fatalf("expected non-user identity to remain nil, got %#v", p.Identity)
	}
}

type httpSubjectEmailClient struct {
	proto.AppProviderClient
}

func (*httpSubjectEmailClient) ResolveHTTPSubject(context.Context, *proto.ResolveHTTPSubjectRequest, ...grpc.CallOption) (*proto.ResolveHTTPSubjectResponse, error) {
	return &proto.ResolveHTTPSubjectResponse{
		Subject: &proto.SubjectContext{
			Id:    "user:user-456",
			Email: "spoofed@example.com",
		},
	}, nil
}

func TestRemoteProviderResolveHTTPSubjectIgnoresProviderReturnedEmail(t *testing.T) {
	t.Parallel()

	prov := &remoteProviderBase{client: &httpSubjectEmailClient{}}
	resolved, err := prov.ResolveHTTPSubject(context.Background(), &core.HTTPSubjectResolveRequest{
		VerifiedSubject: "host:user-456",
	})
	if err != nil {
		t.Fatalf("ResolveHTTPSubject: %v", err)
	}
	if resolved == nil {
		t.Fatal("expected resolved subject")
		return
	}
	if resolved.ID != "user:user-456" || resolved.Kind != "user" {
		t.Fatalf("unexpected resolved subject: %#v", resolved)
	}
}

func TestPrincipalFromProtoDerivesKindFromSubjectID(t *testing.T) {
	t.Parallel()

	p := appaccessservice.PrincipalFromSubjectContext(&proto.SubjectContext{
		Id: "service_account:external-installation-127579767",
	})
	if p == nil {
		t.Fatal("expected principal")
	}
	if p.Kind != principal.Kind("service_account") {
		t.Fatalf("kind = %q, want service_account", p.Kind)
	}
	if p.SubjectID != "service_account:external-installation-127579767" {
		t.Fatalf("subject id = %q", p.SubjectID)
	}
}

func TestRemoteProviderManualAuthOnly(t *testing.T) {
	t.Parallel()

	client := newAppProviderClient(t, &unsupportedCapabilityProviderServer{})
	prov, err := NewRemote(context.Background(), client, manualOnlyStaticSpec(), nil)
	if err != nil {
		t.Fatalf("NewRemote: %v", err)
	}

	if got := prov.AuthTypes(); len(got) != 1 || got[0] != "manual" {
		t.Fatalf("AuthTypes = %#v, want [manual]", got)
	}
	cat := prov.Catalog()
	if cat == nil || len(cat.Operations) != 1 {
		t.Fatalf("unexpected catalog: %+v", cat)
	}
	if cat.Operations[0].Transport != catalog.TransportApp {
		t.Fatalf("Transport = %q, want %q", cat.Operations[0].Transport, catalog.TransportApp)
	}
}

type metadataFailureProviderServer struct {
	proto.UnimplementedAppProviderServer
}

func (s *metadataFailureProviderServer) GetMetadata(context.Context, *emptypb.Empty) (*proto.ProviderMetadata, error) {
	return nil, status.Error(codes.Unknown, "provider metadata: metadata exploded")
}

type unsupportedCapabilityProviderServer struct {
	proto.UnimplementedAppProviderServer
	metadataErr         error
	sessionCatalogCalls atomic.Int32
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
	return &proto.OperationResult{Status: http.StatusOK, Body: []byte(`{}`)}, nil
}

func (s *unsupportedCapabilityProviderServer) GetSessionCatalog(context.Context, *proto.GetSessionCatalogRequest) (*proto.GetSessionCatalogResponse, error) {
	s.sessionCatalogCalls.Add(1)
	return nil, status.Error(codes.Unimplemented, "session catalog is not implemented")
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
			prov, err := NewRemote(context.Background(), newAppProviderClient(t, server), manualOnlyStaticSpec(), nil)
			if err != nil {
				t.Fatalf("NewRemote: %v", err)
			}
			if _, ok := prov.(core.SessionCatalogProvider); !ok {
				t.Fatal("expected remote provider to implement SessionCatalogProvider")
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

			if got := server.sessionCatalogCalls.Load(); got != 0 {
				t.Fatalf("GetSessionCatalog calls = %d, want 0", got)
			}
		})
	}
}

func TestNewRemoteLabelsMetadataFailures(t *testing.T) {
	t.Parallel()

	client := newAppProviderClient(t, &metadataFailureProviderServer{})
	_, err := NewRemote(context.Background(), client, roundTripStaticSpec(), nil)
	if err == nil {
		t.Fatal("expected NewRemote to fail")
	}
	if got := err.Error(); got != `get provider metadata: rpc error: code = Unknown desc = provider metadata: metadata exploded` {
		t.Fatalf("NewRemote error = %q", got)
	}
}

func TestGetAppProviderSupportWithRetryLabelsContextDoneFailures(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	_, err := getAppProviderSupportWithRetry(ctx, &unavailableMetadataProviderServer{cancel: cancel})
	if err == nil {
		t.Fatal("expected getAppProviderSupportWithRetry to fail")
	}
	if got := err.Error(); got != `get provider metadata: rpc error: code = Unavailable desc = metadata warming up` {
		t.Fatalf("getAppProviderSupportWithRetry error = %q", got)
	}
}

type workflowDeclarationsMetadataServer struct {
	proto.UnimplementedAppProviderServer
	specs [][]byte
}

func (s *workflowDeclarationsMetadataServer) GetMetadata(context.Context, *emptypb.Empty) (*proto.ProviderMetadata, error) {
	return &proto.ProviderMetadata{WorkflowDefinitionSpecs: s.specs}, nil
}

func (s *workflowDeclarationsMetadataServer) StartProvider(context.Context, *proto.StartProviderRequest) (*proto.StartProviderResponse, error) {
	return &proto.StartProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *workflowDeclarationsMetadataServer) Execute(context.Context, *proto.ExecuteRequest) (*proto.OperationResult, error) {
	return &proto.OperationResult{Status: http.StatusOK, Body: []byte(`{}`)}, nil
}

func TestRemoteProviderDeclaredWorkflowDefinitions(t *testing.T) {
	t.Parallel()

	spec := &proto.WorkflowDefinitionSpec{Id: "daily", RunAs: "service_account:sa1"}
	encoded, err := gproto.Marshal(spec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	server := &workflowDeclarationsMetadataServer{specs: [][]byte{encoded}}
	prov, err := NewRemote(context.Background(), newAppProviderClient(t, server), manualOnlyStaticSpec(), nil)
	if err != nil {
		t.Fatalf("NewRemote: %v", err)
	}
	decls := DeclaredWorkflowDefinitions(prov)
	if len(decls) != 1 || decls[0].GetId() != "daily" {
		t.Fatalf("declarations = %#v", decls)
	}

	_, err = NewRemote(context.Background(), newAppProviderClient(t, &workflowDeclarationsMetadataServer{
		specs: [][]byte{{0xff}},
	}), manualOnlyStaticSpec(), nil)
	if err == nil {
		t.Fatal("expected corrupt workflow_definition_specs to fail NewRemote")
	}
}
