package invocation

import (
	"context"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func mustAuthorizer(t *testing.T, cfg config.AuthorizationConfig, providers *registry.ProviderMap[core.Provider]) *authorization.Authorizer {
	t.Helper()
	authz, err := authorization.New(cfg, nil, providers, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	return authz
}

func TestBrokerResolveToken_ConnectionModeNoneResolvesSessionUserSubject(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "weather",
			ConnMode: core.ConnectionModeNone,
		}),
		svc.Users,
		svc.Tokens,
	)
	p := &principal.Principal{
		Identity: &core.UserIdentity{
			Email: "user@example.com",
		},
		Kind:   principal.KindUser,
		Source: principal.SourceSession,
	}

	ctx, token, err := broker.ResolveToken(context.Background(), p, "weather", "", "")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if token != "" {
		t.Fatalf("token = %q, want empty", token)
	}
	if p.UserID == "" {
		t.Fatal("expected resolved user ID")
	}
	if got := p.SubjectID; got != principal.UserSubjectID(p.UserID) {
		t.Fatalf("subject ID = %q, want %q", got, principal.UserSubjectID(p.UserID))
	}
	if got := CredentialContextFromContext(ctx).Mode; got != core.ConnectionModeNone {
		t.Fatalf("credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
}

func TestBrokerResolveToken_WorkflowContextDoesNotBypassWorkloadIdentityBinding(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeUser,
	})
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"workflow.roadmap": {
				Token: "gst_wld_workflow-roadmap",
				Providers: map[string]config.WorkloadProviderDef{
					"slack": {
						Connection: "workspace",
						Instance:   "team-a",
						Allow:      []string{"list"},
					},
				},
			},
		},
	}, providers)
	broker := NewBroker(providers, svc.Users, svc.Tokens, WithAuthorizer(authz))

	if err := svc.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
		ID:          "identity-workspace-team-a",
		SubjectID:   principal.IdentitySubjectID(),
		Integration: "slack",
		Connection:  "workspace",
		Instance:    "team-a",
		AccessToken: "team-a-token",
	}); err != nil {
		t.Fatalf("StoreToken team-a: %v", err)
	}
	if err := svc.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
		ID:          "identity-workspace-team-b",
		SubjectID:   principal.IdentitySubjectID(),
		Integration: "slack",
		Connection:  "workspace",
		Instance:    "team-b",
		AccessToken: "team-b-token",
	}); err != nil {
		t.Fatalf("StoreToken team-b: %v", err)
	}

	workload := &principal.Principal{
		SubjectID: principal.WorkloadSubjectID("workflow.roadmap"),
		Source:    principal.SourceWorkloadToken,
	}
	ctx := WithWorkflowContext(context.Background(), map[string]any{
		"runId": "run-123",
	})

	_, _, err := broker.ResolveToken(ctx, workload, "slack", "workspace", "team-b")
	if err == nil {
		t.Fatal("expected binding override to be rejected")
	}
	if got, want := err.Error(), "static identity-token callers may not override connection or instance bindings"; got == "" || !strings.Contains(got, want) {
		t.Fatalf("ResolveToken error = %q, want substring %q", got, want)
	}
}

func TestBrokerResolveToken_ManagedIdentityRejectsSelectorOverrides(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeUser,
	})
	authz := mustAuthorizer(t, config.AuthorizationConfig{}, providers)
	broker := NewBroker(providers, svc.Users, svc.Tokens, WithAuthorizer(authz))

	if err := svc.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
		ID:          "managed-default",
		SubjectID:   principal.ManagedIdentitySubjectID("managed-1"),
		Integration: "slack",
		Connection:  "",
		Instance:    "default",
		AccessToken: "managed-token",
	}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	managed := &principal.Principal{
		SubjectID:        principal.ManagedIdentitySubjectID("managed-1"),
		Source:           principal.SourceAPIToken,
		Scopes:           []string{"slack"},
		TokenPermissions: principal.PermissionsFromScopeString("slack"),
	}

	_, token, err := broker.ResolveToken(context.Background(), managed, "slack", "", "")
	if err != nil {
		t.Fatalf("ResolveToken without selectors: %v", err)
	}
	if token != "managed-token" {
		t.Fatalf("token = %q, want %q", token, "managed-token")
	}

	_, _, err = broker.ResolveToken(context.Background(), managed, "slack", "", "default")
	if err == nil {
		t.Fatal("expected managed identity selector override to be rejected")
	}
	if got, want := err.Error(), "static identity-token callers may not override connection or instance bindings"; got == "" || !strings.Contains(got, want) {
		t.Fatalf("ResolveToken error = %q, want substring %q", got, want)
	}
}
