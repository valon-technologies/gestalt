package invocation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestBrokerResolveToken_ConnectionModeIdentityUsesCanonicalIdentityStore(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	if err := svc.Tokens.StoreIdentityToken(context.Background(), &core.IntegrationToken{
		ID:          "shared-identity-default",
		IdentityID:  principal.IdentityPrincipal,
		Integration: "slack",
		Connection:  "workspace",
		Instance:    "default",
		AccessToken: "identity-token",
	}); err != nil {
		t.Fatalf("StoreIdentityToken: %v", err)
	}

	broker := NewBroker(
		testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeIdentity,
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

	ctx, token, err := broker.ResolveToken(context.Background(), p, "slack", "workspace", "")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if token != "identity-token" {
		t.Fatalf("token = %q, want %q", token, "identity-token")
	}
	cred := CredentialContextFromContext(ctx)
	if cred.Mode != core.ConnectionModeIdentity {
		t.Fatalf("credential mode = %q, want %q", cred.Mode, core.ConnectionModeIdentity)
	}
	if cred.SubjectID != principal.IdentitySubjectID(principal.IdentityPrincipal) {
		t.Fatalf("credential subject = %q, want %q", cred.SubjectID, principal.IdentitySubjectID(principal.IdentityPrincipal))
	}
}

func TestBrokerResolveToken_WorkflowContextDoesNotBypassWorkloadIdentityBinding(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeIdentity,
	})
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		IdentityTokens: map[string]config.WorkloadDef{
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

	if err := svc.Tokens.StoreIdentityToken(context.Background(), &core.IntegrationToken{
		ID:          "identity-workspace-team-a",
		IdentityID:  "workflow.roadmap",
		Integration: "slack",
		Connection:  "workspace",
		Instance:    "team-a",
		AccessToken: "team-a-token",
	}); err != nil {
		t.Fatalf("StoreToken team-a: %v", err)
	}
	if err := svc.Tokens.StoreIdentityToken(context.Background(), &core.IntegrationToken{
		ID:          "identity-workspace-team-b",
		IdentityID:  "workflow.roadmap",
		Integration: "slack",
		Connection:  "workspace",
		Instance:    "team-b",
		AccessToken: "team-b-token",
	}); err != nil {
		t.Fatalf("StoreToken team-b: %v", err)
	}

	workload := &principal.Principal{
		SubjectID: principal.WorkloadSubjectID("workflow.roadmap"),
		Kind:      principal.KindIdentity,
		Source:    principal.SourceIdentityToken,
	}
	ctx := WithWorkflowContext(context.Background(), map[string]any{
		"runId": "run-123",
	})

	_, _, err := broker.ResolveToken(ctx, workload, "slack", "workspace", "team-b")
	if err == nil {
		t.Fatal("expected binding override to be rejected")
	}
	if got, want := err.Error(), "identity-token callers may not override connection or instance bindings"; got == "" || !strings.Contains(got, want) {
		t.Fatalf("ResolveToken error = %q, want substring %q", got, want)
	}
}

type stubOAuthRefresher struct {
	refreshTokenFn func(context.Context, string) (*core.TokenResponse, error)
}

func (s stubOAuthRefresher) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return s.refreshTokenFn(ctx, refreshToken)
}

func (s stubOAuthRefresher) RefreshTokenWithURL(ctx context.Context, refreshToken, _ string) (*core.TokenResponse, error) {
	return s.refreshTokenFn(ctx, refreshToken)
}

func (stubOAuthRefresher) TokenURL() string { return "" }

func TestBrokerResolveToken_RefreshesIdentityOwnedWorkloadToken(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeIdentity,
	})
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		IdentityTokens: map[string]config.WorkloadDef{
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
	expired := time.Now().Add(-1 * time.Hour)
	if err := svc.Tokens.StoreIdentityToken(context.Background(), &core.IntegrationToken{
		ID:           "identity-workspace-team-a",
		IdentityID:   "workflow.roadmap",
		Integration:  "slack",
		Connection:   "workspace",
		Instance:     "team-a",
		AccessToken:  "stale-token",
		RefreshToken: "refresh-me",
		ExpiresAt:    &expired,
	}); err != nil {
		t.Fatalf("StoreIdentityToken: %v", err)
	}

	broker := NewBroker(
		providers,
		svc.Users,
		svc.Tokens,
		WithAuthorizer(authz),
		WithConnectionAuth(func() map[string]map[string]OAuthRefresher {
			return map[string]map[string]OAuthRefresher{
				"slack": {
					"workspace": stubOAuthRefresher{
						refreshTokenFn: func(_ context.Context, refreshToken string) (*core.TokenResponse, error) {
							if refreshToken != "refresh-me" {
								return nil, errors.New("unexpected refresh token")
							}
							return &core.TokenResponse{
								AccessToken:  "fresh-token",
								RefreshToken: "rotated-refresh",
								ExpiresIn:    3600,
							}, nil
						},
					},
				},
			}
		}),
	)

	workload := &principal.Principal{
		SubjectID: principal.WorkloadSubjectID("workflow.roadmap"),
		Kind:      principal.KindIdentity,
		Source:    principal.SourceIdentityToken,
	}
	_, token, err := broker.ResolveToken(context.Background(), workload, "slack", "workspace", "")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if token != "fresh-token" {
		t.Fatalf("token = %q, want %q", token, "fresh-token")
	}

	stored, err := svc.Tokens.IdentityToken(context.Background(), "workflow.roadmap", "slack", "workspace", "team-a")
	if err != nil {
		t.Fatalf("IdentityToken: %v", err)
	}
	if stored.AccessToken != "fresh-token" {
		t.Fatalf("stored access token = %q, want %q", stored.AccessToken, "fresh-token")
	}
	if stored.RefreshToken != "rotated-refresh" {
		t.Fatalf("stored refresh token = %q, want %q", stored.RefreshToken, "rotated-refresh")
	}

	if _, err := svc.Tokens.Token(context.Background(), "", "slack", "workspace", "team-a"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("legacy empty-user token lookup err = %v, want %v", err, core.ErrNotFound)
	}
}
