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

func mustAuthorizerWithDefaults(t *testing.T, cfg config.AuthorizationConfig, providers *registry.ProviderMap[core.Provider], defaultConnections map[string]string) *authorization.Authorizer {
	t.Helper()
	authz, err := authorization.New(cfg, nil, providers, defaultConnections)
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
		Kind:      principal.KindWorkload,
		Source:    principal.SourceWorkloadToken,
	}
	ctx := WithWorkflowContext(context.Background(), map[string]any{
		"runId": "run-123",
	})

	_, _, err := broker.ResolveToken(ctx, workload, "slack", "workspace", "team-b")
	if err == nil {
		t.Fatal("expected binding override to be rejected")
	}
	if got, want := err.Error(), "workloads may not override connection or instance bindings"; got == "" || !strings.Contains(got, want) {
		t.Fatalf("ResolveToken error = %q, want substring %q", got, want)
	}
}

func TestBrokerResolveToken_ManagedIdentityDoesNotBypassSelectorBinding(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeUser,
	})
	authz := mustAuthorizer(t, config.AuthorizationConfig{}, providers)
	broker := NewBroker(providers, svc.Users, svc.Tokens, WithAuthorizer(authz))

	identity := &principal.Principal{
		Kind:                principal.KindWorkload,
		SubjectID:           principal.ManagedIdentitySubjectID("ops-bot"),
		CredentialSubjectID: principal.UserSubjectID("creator-user"),
		Source:              principal.SourceAPIToken,
	}

	_, _, err := broker.ResolveToken(context.Background(), identity, "slack", "workspace", "team-b")
	if err == nil {
		t.Fatal("expected managed identity selector override to be rejected")
	}
	if got, want := err.Error(), "workloads may not override connection or instance bindings"; got == "" || !strings.Contains(got, want) {
		t.Fatalf("ResolveToken error = %q, want substring %q", got, want)
	}
}

func TestBrokerResolveToken_ManagedIdentityUsesDefaultBindingSelectors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                 string
		mode                 core.ConnectionMode
		credentialSubjectID  string
		storedTokenSubjectID string
		wantMode             core.ConnectionMode
	}{
		{
			name:                 "user",
			mode:                 core.ConnectionModeUser,
			credentialSubjectID:  principal.UserSubjectID("user-123"),
			storedTokenSubjectID: principal.UserSubjectID("user-123"),
			wantMode:             core.ConnectionModeUser,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := coretesting.NewStubServices(t)
			providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "sampledb",
				ConnMode: tc.mode,
			})
			authz := mustAuthorizerWithDefaults(t, config.AuthorizationConfig{}, providers, map[string]string{
				"sampledb": "workspace",
			})
			broker := NewBroker(providers, svc.Users, svc.Tokens, WithAuthorizer(authz))

			if err := svc.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
				ID:          "tok-default",
				SubjectID:   tc.storedTokenSubjectID,
				Integration: "sampledb",
				Connection:  "workspace",
				Instance:    "default",
				AccessToken: "bound-token",
			}); err != nil {
				t.Fatalf("StoreToken: %v", err)
			}

			p := &principal.Principal{
				SubjectID:           principal.ManagedIdentitySubjectID("bot-123"),
				CredentialSubjectID: tc.credentialSubjectID,
				Kind:                principal.KindWorkload,
				Source:              principal.SourceWorkloadToken,
				TokenPermissions: principal.CompilePermissions([]core.AccessPermission{
					{Plugin: "sampledb"},
				}),
			}

			ctx, token, err := broker.ResolveToken(context.Background(), p, "sampledb", "", "")
			if err != nil {
				t.Fatalf("ResolveToken: %v", err)
			}
			if token != "bound-token" {
				t.Fatalf("token = %q, want %q", token, "bound-token")
			}
			credentialCtx := CredentialContextFromContext(ctx)
			if credentialCtx.Connection != "workspace" {
				t.Fatalf("connection = %q, want %q", credentialCtx.Connection, "workspace")
			}
			if credentialCtx.Instance != "default" {
				t.Fatalf("instance = %q, want %q", credentialCtx.Instance, "default")
			}
			if credentialCtx.Mode != tc.wantMode {
				t.Fatalf("mode = %q, want %q", credentialCtx.Mode, tc.wantMode)
			}
			if credentialCtx.SubjectID != tc.storedTokenSubjectID {
				t.Fatalf("subjectID = %q, want %q", credentialCtx.SubjectID, tc.storedTokenSubjectID)
			}
		})
	}
}
