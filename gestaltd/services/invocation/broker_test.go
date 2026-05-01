package invocation

import (
	"context"
	"fmt"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/testutil"
)

func TestBrokerResolveToken_ConnectionModeNoneResolvesSessionUserSubject(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "weather",
			ConnMode: core.ConnectionModeNone,
		}),
		svc.Users,
		svc.ExternalCredentials,
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

func TestBrokerResolveToken_NonUserSubjectUsesOwnExternalCredential(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeUser,
	})
	broker := NewBroker(providers, svc.Users, svc.ExternalCredentials)
	subjectID := "service_account:workflow-roadmap"

	if err := svc.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
		ID:          "subject-workspace-team-a",
		SubjectID:   subjectID,
		Integration: "slack",
		Connection:  "workspace",
		Instance:    "team-a",
		AccessToken: "team-a-token",
	}); err != nil {
		t.Fatalf("PutCredential team-a: %v", err)
	}
	if err := svc.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
		ID:          "subject-workspace-team-b",
		SubjectID:   subjectID,
		Integration: "slack",
		Connection:  "workspace",
		Instance:    "team-b",
		AccessToken: "team-b-token",
	}); err != nil {
		t.Fatalf("PutCredential team-b: %v", err)
	}

	subject := &principal.Principal{
		SubjectID: subjectID,
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	}
	ctx := WithWorkflowContext(context.Background(), map[string]any{
		"runId": "run-123",
	})

	ctx, token, err := broker.ResolveToken(ctx, subject, "slack", "workspace", "team-b")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if token != "team-b-token" {
		t.Fatalf("token = %q, want team-b-token", token)
	}
	cred := CredentialContextFromContext(ctx)
	if cred.SubjectID != subjectID {
		t.Fatalf("credential subject = %q, want %q", cred.SubjectID, subjectID)
	}
	if cred.Connection != "workspace" || cred.Instance != "team-b" {
		t.Fatalf("credential selectors = %q/%q, want workspace/team-b", cred.Connection, cred.Instance)
	}
}

func TestBrokerResolveToken_AllowsInternalConnectionWhenContextAuthorized(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeUser,
		}),
		svc.Users,
		svc.ExternalCredentials,
		WithConnectionRuntime(ConnectionRuntimeMap{
			"slack": {
				"bot": {
					Mode:     core.ConnectionModePlatform,
					Exposure: core.ConnectionExposureInternal,
					Token:    "bot-token",
				},
			},
		}.Resolve),
	)
	subject := &principal.Principal{
		SubjectID: "service_account:workflow-config",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	}

	_, _, err := broker.ResolveToken(context.Background(), subject, "slack", "bot", "")
	if err == nil {
		t.Fatal("ResolveToken without internal connection access succeeded, want denial")
	}

	ctx, token, err := broker.ResolveToken(WithInternalConnectionAccess(context.Background()), subject, "slack", "bot", "")
	if err != nil {
		t.Fatalf("ResolveToken with internal connection access: %v", err)
	}
	if token != "bot-token" {
		t.Fatalf("token = %q, want bot-token", token)
	}
	if cred := CredentialContextFromContext(ctx); cred.Mode != core.ConnectionModePlatform || cred.Connection != "bot" {
		t.Fatalf("credential context = %#v, want platform bot", cred)
	}
}

func TestBrokerInvokeProviderOverrideResolvesOperationConnectionFromOverride(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	cat := &catalog.Catalog{
		Name: "slack",
		Operations: []catalog.CatalogOperation{{
			ID:     "send",
			Method: "POST",
		}},
	}
	baseProvider := &brokerOperationConnectionProvider{
		StubIntegration: &coretesting.StubIntegration{
			N:          "slack",
			ConnMode:   core.ConnectionModeUser,
			CatalogVal: cat,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: 200, Body: token}, nil
			},
		},
		operationConnections: map[string]string{"send": "default"},
	}
	overrideProvider := &brokerOperationConnectionProvider{
		StubIntegration: &coretesting.StubIntegration{
			N:          "slack",
			ConnMode:   core.ConnectionModeUser,
			CatalogVal: cat,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: 200, Body: token}, nil
			},
		},
		operationConnections: map[string]string{"send": "default"},
		selector: core.OperationConnectionSelector{
			Parameter: "actor",
			Default:   "user",
			Values: map[string]string{
				"bot":  "bot",
				"user": "default",
			},
		},
	}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, baseProvider),
		svc.Users,
		svc.ExternalCredentials,
		WithProviderOverrides(staticProviderOverrideResolver{provider: overrideProvider}),
	)
	subjectID := principal.UserSubjectID("u-override")
	for connection, token := range map[string]string{
		"default": "user-token",
		"bot":     "bot-token",
	} {
		if err := svc.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
			SubjectID:   subjectID,
			Integration: "slack",
			Connection:  connection,
			Instance:    "default",
			AccessToken: token,
		}); err != nil {
			t.Fatalf("PutCredential(%s): %v", connection, err)
		}
	}

	result, err := broker.Invoke(
		context.Background(),
		&principal.Principal{SubjectID: subjectID, UserID: "u-override", Kind: principal.KindUser},
		"slack",
		"",
		"send",
		map[string]any{"actor": "bot"},
	)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Body != "bot-token" {
		t.Fatalf("token = %q, want bot-token", result.Body)
	}
}

type staticProviderOverrideResolver struct {
	provider core.Provider
}

func (r staticProviderOverrideResolver) ResolveProviderOverride(context.Context, *principal.Principal, string) (core.Provider, bool, error) {
	return r.provider, r.provider != nil, nil
}

type brokerOperationConnectionProvider struct {
	*coretesting.StubIntegration
	operationConnections map[string]string
	selector             core.OperationConnectionSelector
}

func (p *brokerOperationConnectionProvider) ConnectionForOperation(operation string) string {
	return p.operationConnections[operation]
}

func (p *brokerOperationConnectionProvider) ResolveConnectionForOperation(operation string, params map[string]any) (string, error) {
	if p.selector.Parameter == "" {
		return p.ConnectionForOperation(operation), nil
	}
	selected := p.selector.Default
	if params != nil {
		if raw, ok := params[p.selector.Parameter]; ok {
			selected = fmt.Sprint(raw)
		}
	}
	connection, ok := p.selector.Values[selected]
	if !ok {
		return "", fmt.Errorf("unsupported selector value %q", selected)
	}
	return connection, nil
}
