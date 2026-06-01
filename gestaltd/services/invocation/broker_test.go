package invocation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
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
		ConnMode: core.ConnectionModeSubject,
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

func TestBrokerInvokeRejectsExplicitOperationConnectionOverride(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	cat := &catalog.Catalog{
		Name: "gmail",
		Operations: []catalog.CatalogOperation{{
			ID:     "gmail.users.messages.modify",
			Method: "POST",
		}},
	}
	executed := false
	provider := &brokerOperationConnectionProvider{
		StubIntegration: &coretesting.StubIntegration{
			N:          "gmail",
			ConnMode:   core.ConnectionModeSubject,
			CatalogVal: cat,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				executed = true
				return &core.OperationResult{Status: 200}, nil
			},
		},
		operationConnections: map[string]string{"gmail.users.messages.modify": "default"},
	}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		svc.ExternalCredentials,
	)

	_, err := broker.Invoke(
		WithConnection(context.Background(), "override"),
		&principal.Principal{SubjectID: principal.UserSubjectID("u-gmail"), UserID: "u-gmail", Kind: principal.KindUser},
		"gmail",
		"",
		"gmail.users.messages.modify",
		nil,
	)
	if err == nil {
		t.Fatal("Invoke succeeded, want connection override rejection")
	}
	if !errors.Is(err, ErrInvalidInvocation) {
		t.Fatalf("Invoke error = %v, want ErrInvalidInvocation", err)
	}
	if !strings.Contains(err.Error(), `uses connection "default"`) {
		t.Fatalf("Invoke error = %v, want operation connection detail", err)
	}
	if executed {
		t.Fatal("Execute was called after rejected connection override")
	}
}

func TestBrokerInvokeAllowsExplicitConnectionForResolvedPluginTransport(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	executed := false
	provider := &brokerOperationConnectionProvider{
		StubIntegration: &coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "slack",
				Operations: []catalog.CatalogOperation{{
					ID:     "chat.postMessage",
					Method: "POST",
				}},
			},
			ExecuteFn: func(_ context.Context, operation string, _ map[string]any, token string) (*core.OperationResult, error) {
				executed = true
				if operation != "assistant.reconcileStuckRequests" {
					t.Fatalf("operation = %q, want assistant.reconcileStuckRequests", operation)
				}
				if token != "" {
					t.Fatalf("token = %q, want empty", token)
				}
				return &core.OperationResult{Status: 200, Body: "ok"}, nil
			},
		},
		operationConnections: map[string]string{"assistant.reconcileStuckRequests": "default"},
	}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		svc.ExternalCredentials,
	)
	ctx := WithCatalogOperation(
		WithConnection(context.Background(), "bot"),
		"slack",
		catalog.CatalogOperation{
			ID:        "assistant.reconcileStuckRequests",
			Method:    "POST",
			Transport: catalog.TransportApp,
		},
	)

	result, err := broker.Invoke(
		ctx,
		&principal.Principal{SubjectID: "service_account:workflow-config", Kind: principal.Kind("service_account")},
		"slack",
		"",
		"assistant.reconcileStuckRequests",
		nil,
	)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Body != "ok" {
		t.Fatalf("result body = %q, want ok", result.Body)
	}
	if !executed {
		t.Fatal("Execute was not called")
	}
}

func TestOperationConnectionOverrideAllowedForPluginTransportOperations(t *testing.T) {
	t.Parallel()

	prov := &brokerOperationConnectionProvider{
		StubIntegration: &coretesting.StubIntegration{
			N: "slack",
			CatalogVal: &catalog.Catalog{
				Name: "slack",
				Operations: []catalog.CatalogOperation{
					{ID: "assistant.reconcileStuckRequests", Method: "POST", Transport: catalog.TransportApp},
					{ID: "chat.postMessage", Method: "POST", Transport: catalog.TransportREST},
				},
			},
		},
		operationConnections: map[string]string{
			"assistant.reconcileStuckRequests": "default",
			"chat.postMessage":                 "default",
		},
	}

	if !OperationConnectionOverrideAllowed(prov, "assistant.reconcileStuckRequests", nil) {
		t.Fatal("plugin transport operation should allow explicit connection override")
	}
	if OperationConnectionOverrideAllowed(prov, "chat.postMessage", nil) {
		t.Fatal("REST operation should not allow explicit connection override")
	}
}

type brokerOperationConnectionProvider struct {
	*coretesting.StubIntegration
	operationConnections map[string]string
	selector             core.OperationConnectionSelector
	allowOverride        bool
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

func (p *brokerOperationConnectionProvider) OperationConnectionOverrideAllowed(string, map[string]any) bool {
	return p.allowOverride
}
