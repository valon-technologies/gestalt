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

func TestBrokerResolveToken_PlatformConnectionAppliesRuntimeParams(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "looker",
			ConnMode: core.ConnectionModePlatform,
		}),
		svc.Users,
		svc.ExternalCredentials,
		WithConnectionRuntime(ConnectionRuntimeMap{
			"looker": {
				core.PluginConnectionName: {
					Mode:  core.ConnectionModePlatform,
					Token: "platform-token",
					Params: map[string]string{
						"host": "valon.cloud.looker.com",
					},
				},
			},
		}.Resolve),
	)
	subject := &principal.Principal{
		SubjectID: "service_account:workflow-config",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	}

	ctx, token, err := broker.ResolveToken(context.Background(), subject, "looker", "", "")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if token != "platform-token" {
		t.Fatalf("token = %q, want platform-token", token)
	}
	if got := core.ConnectionParams(ctx)["host"]; got != "valon.cloud.looker.com" {
		t.Fatalf("connection param host = %q, want valon.cloud.looker.com", got)
	}
}

func TestBrokerResolveToken_PlatformConnectionUsesStaticTokenWithoutExternalProvider(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "looker",
			ConnMode: core.ConnectionModePlatform,
		}),
		svc.Users,
		nil,
		WithConnectionRuntime(ConnectionRuntimeMap{
			"looker": {
				core.PluginConnectionName: {
					Mode:  core.ConnectionModePlatform,
					Token: "static-platform-token",
				},
			},
		}.Resolve),
	)

	ctx, token, err := broker.ResolveToken(context.Background(), &principal.Principal{
		SubjectID: "service_account:workflow-config",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	}, "looker", "", "")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if token != "static-platform-token" {
		t.Fatalf("token = %q, want static-platform-token", token)
	}
	if cred := CredentialContextFromContext(ctx); cred.Mode != core.ConnectionModePlatform || cred.SubjectID != platformSubjectID {
		t.Fatalf("credential context = %#v, want platform static credential", cred)
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
			ConnMode:   core.ConnectionModeUser,
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
		WithConnection(context.Background(), "platform"),
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

func TestBrokerInvokeRejectsExplicitInternalConnectionOverride(t *testing.T) {
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
			ConnMode:   core.ConnectionModeUser,
			CatalogVal: cat,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				executed = true
				return &core.OperationResult{Status: 200}, nil
			},
		},
		operationConnections: map[string]string{"gmail.users.messages.modify": "default"},
		allowOverride:        true,
	}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		svc.ExternalCredentials,
		WithConnectionRuntime(ConnectionRuntimeMap{
			"gmail": {
				"default":  {Mode: core.ConnectionModeUser},
				"platform": {Mode: core.ConnectionModePlatform, Exposure: core.ConnectionExposureInternal},
			},
		}.Resolve),
	)

	_, err := broker.Invoke(
		WithConnection(context.Background(), "platform"),
		&principal.Principal{SubjectID: principal.UserSubjectID("u-gmail"), UserID: "u-gmail", Kind: principal.KindUser},
		"gmail",
		"",
		"gmail.users.messages.modify",
		nil,
	)
	if err == nil {
		t.Fatal("Invoke succeeded, want internal connection override rejection")
	}
	if !errors.Is(err, ErrInvalidInvocation) {
		t.Fatalf("Invoke error = %v, want ErrInvalidInvocation", err)
	}
	if !strings.Contains(err.Error(), `instead of "platform"`) {
		t.Fatalf("Invoke error = %v, want explicit platform connection detail", err)
	}
	if executed {
		t.Fatal("Execute was called after rejected internal connection override")
	}
}

func TestBrokerAgentExternalIdentitySkipsIncompleteConnectionMatchForFallback(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	broker := NewBroker(nil, svc.Users, svc.ExternalCredentials)
	subjectID := principal.UserSubjectID("agent-user")

	if err := svc.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
		SubjectID:    subjectID,
		Integration:  "github",
		Connection:   "workspace",
		AccessToken:  "invalid-token",
		MetadataJSON: `{"gestalt.external_identity.id":"222"}`,
	}); err != nil {
		t.Fatalf("PutCredential invalid: %v", err)
	}
	if err := svc.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
		SubjectID:    subjectID,
		Integration:  "github",
		Connection:   "fallback",
		AccessToken:  "valid-token",
		MetadataJSON: `{"gestalt.external_identity.type":"github_user","gestalt.external_identity.id":"333"}`,
	}); err != nil {
		t.Fatalf("PutCredential fallback: %v", err)
	}

	identity, err := broker.agentExternalIdentity(context.Background(), subjectID, "github", "workspace")
	if err != nil {
		t.Fatalf("agentExternalIdentity: %v", err)
	}
	if identity.Type != "github_user" || identity.ID != "333" {
		t.Fatalf("identity = %#v, want github_user/333", identity)
	}
}

func TestBrokerInvokeGraphQLEnrichesAgentExternalIdentity(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	agentSubjectID := principal.UserSubjectID("agent-user")
	if err := svc.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
		SubjectID:    agentSubjectID,
		Integration:  "github",
		AccessToken:  "github-token",
		MetadataJSON: `{"gestalt.external_identity.type":"github_user","gestalt.external_identity.id":"222"}`,
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	var captured ExternalIdentityContext
	provider := &brokerGraphQLProvider{
		StubIntegration: &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
		},
		invokeGraphQLFn: func(ctx context.Context, _ core.GraphQLRequest, _ string) (*core.OperationResult, error) {
			captured = AgentExternalIdentityContextFromContext(ctx)
			return &core.OperationResult{Status: 200}, nil
		},
	}
	broker := NewBroker(testutil.NewProviderRegistry(t, provider), svc.Users, svc.ExternalCredentials)

	ctx := WithRunAsAudit(
		context.Background(),
		&core.RunAsSubject{SubjectID: agentSubjectID, CredentialSubjectID: agentSubjectID, SubjectKind: string(principal.KindUser)},
		&core.RunAsSubject{SubjectID: "service_account:github-bot", CredentialSubjectID: "service_account:github-bot", SubjectKind: "service_account"},
	)
	_, err := broker.InvokeGraphQL(
		ctx,
		&principal.Principal{SubjectID: "service_account:github-bot", CredentialSubjectID: "service_account:github-bot"},
		"github",
		"",
		core.GraphQLRequest{Document: "query { viewer { login } }"},
	)
	if err != nil {
		t.Fatalf("InvokeGraphQL: %v", err)
	}
	if captured.Type != "github_user" || captured.ID != "222" {
		t.Fatalf("agent external identity = %#v, want github_user/222", captured)
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
					{ID: "assistant.reconcileStuckRequests", Method: "POST", Transport: catalog.TransportPlugin},
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

type brokerGraphQLProvider struct {
	*coretesting.StubIntegration
	invokeGraphQLFn func(context.Context, core.GraphQLRequest, string) (*core.OperationResult, error)
}

func (p *brokerGraphQLProvider) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, token string) (*core.OperationResult, error) {
	if p.invokeGraphQLFn != nil {
		return p.invokeGraphQLFn(ctx, request, token)
	}
	return &core.OperationResult{Status: 200}, nil
}
