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
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
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
		Source: principal.SourceBearer,
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

	if err := svc.ExternalCredentials.UpsertCredential(context.Background(), &core.ExternalCredential{
		ID:        "subject-workspace-team-a",
		Subject:   subjectID,
		Audience:  "slack:workspace",
		Qualifier: "team-a",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "team-a-token"},
	}); err != nil {
		t.Fatalf("UpsertCredential team-a: %v", err)
	}
	if err := svc.ExternalCredentials.UpsertCredential(context.Background(), &core.ExternalCredential{
		ID:        "subject-workspace-team-b",
		Subject:   subjectID,
		Audience:  "slack:workspace",
		Qualifier: "team-b",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "team-b-token"},
	}); err != nil {
		t.Fatalf("UpsertCredential team-b: %v", err)
	}

	subject := &principal.Principal{
		SubjectID: subjectID,
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceBearer,
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
				return &core.OperationResult{Status: 200, Body: []byte("ok")}, nil
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
	if string(result.Body) != "ok" {
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

func TestBrokerInvokeChecksAuthorizationBeforeExecution(t *testing.T) {
	t.Parallel()

	executed := false
	provider := &coretesting.StubIntegration{
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
			if operation != "chat.postMessage" {
				t.Fatalf("operation = %q, want chat.postMessage", operation)
			}
			if token != "" {
				t.Fatalf("token = %q, want empty", token)
			}
			return &core.OperationResult{Status: 200, Body: []byte("ok")}, nil
		},
	}
	authz := &recordingAuthorizationProvider{allowed: true}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		nil,
		nil,
		WithAuthorizationProvider(authz),
		WithProviderKinds(map[string]ProviderKind{"slack": ProviderKindApp}),
	)

	result, err := broker.Invoke(
		context.Background(),
		&principal.Principal{SubjectID: "user:u-123", UserID: "u-123", Kind: principal.KindUser},
		"slack",
		"",
		"chat.postMessage",
		map[string]any{"channel": "C123"},
	)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if string(result.Body) != "ok" {
		t.Fatalf("result body = %q, want ok", result.Body)
	}
	if !executed {
		t.Fatal("Execute was not called")
	}
	req := authz.lastCheckAccess
	if req == nil {
		t.Fatal("CheckAccess was not called")
	}
	if got := req.GetSubject().GetType(); got != "subject" {
		t.Fatalf("subject type = %q, want subject", got)
	}
	if got := req.GetSubject().GetId(); got != "user:u-123" {
		t.Fatalf("subject id = %q, want user:u-123", got)
	}
	if got := req.GetResource().GetType(); got != "app" {
		t.Fatalf("resource type = %q, want app", got)
	}
	if got := req.GetResource().GetId(); got != "slack" {
		t.Fatalf("resource id = %q, want slack", got)
	}
	if got := req.GetAction().GetName(); got != "chat.postMessage" {
		t.Fatalf("action name = %q, want chat.postMessage", got)
	}
}

func TestBrokerInvokeSkipsLocalAuthorizationForRemoteDelegatedApps(t *testing.T) {
	t.Parallel()

	executed := false
	provider := &remoteDelegatedAppStub{
		StubIntegration: &coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Operations: []catalog.CatalogOperation{{ID: "issues.list"}},
			},
			ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
				executed = true
				return &core.OperationResult{Status: 200, Body: []byte("ok")}, nil
			},
		},
	}
	authz := &recordingAuthorizationProvider{allowed: false}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		nil,
		nil,
		WithAuthorizationProvider(authz),
	)

	_, err := broker.Invoke(
		context.Background(),
		&principal.Principal{SubjectID: "user:u-123", Kind: principal.KindUser},
		"linear",
		"",
		"issues.list",
		nil,
	)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !executed {
		t.Fatal("Execute was not called")
	}
	if authz.lastCheckAccess != nil {
		t.Fatal("CheckAccess should be skipped for remote-delegated apps")
	}
}

type remoteDelegatedAppStub struct {
	*coretesting.StubIntegration
}

func (p *remoteDelegatedAppStub) RemoteCredentialDelegated() bool { return true }

func TestBrokerCheckProviderAccessAllowsProviderScopedPrincipal(t *testing.T) {
	t.Parallel()

	broker := NewBroker(nil, nil, nil)
	p := &principal.Principal{
		SubjectID: "service_account:workflow-runner",
		Scopes:    []string{"github:issues.triage"},
	}
	if err := broker.CheckProviderAccess(context.Background(), p, "github"); err != nil {
		t.Fatalf("CheckProviderAccess: %v", err)
	}
}

func TestBrokerInvokeGraphQLAuthorizationDeniesBeforeCredentialResolution(t *testing.T) {
	t.Parallel()

	graphQLInvoked := false
	provider := &brokerGraphQLProvider{
		StubIntegration: &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeSubject,
		},
		invokeGraphQL: func(context.Context, core.GraphQLRequest, string) (*core.OperationResult, error) {
			graphQLInvoked = true
			return &core.OperationResult{Status: 200}, nil
		},
	}
	authz := &recordingAuthorizationProvider{allowed: false}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		nil,
		nil,
		WithAuthorizationProvider(authz),
		WithProviderKinds(map[string]ProviderKind{"github": ProviderKindApp}),
	)

	_, err := broker.InvokeGraphQL(
		context.Background(),
		&principal.Principal{SubjectID: "service_account:reports", Kind: principal.Kind("service_account")},
		"github",
		"",
		GraphQLRequest{Document: "query { viewer { login } }"},
	)
	if err == nil {
		t.Fatal("InvokeGraphQL succeeded, want authorization denied")
	}
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("InvokeGraphQL error = %v, want ErrAuthorizationDenied", err)
	}
	if graphQLInvoked {
		t.Fatal("InvokeGraphQL provider was called after authorization denial")
	}
	req := authz.lastCheckAccess
	if req == nil {
		t.Fatal("CheckAccess was not called")
	}
	if got := req.GetSubject().GetType(); got != "subject" {
		t.Fatalf("subject type = %q, want subject", got)
	}
	if got := req.GetSubject().GetId(); got != "service_account:reports" {
		t.Fatalf("subject id = %q, want service_account:reports", got)
	}
	if got := req.GetResource().GetType(); got != "app" {
		t.Fatalf("resource type = %q, want app", got)
	}
	if got := req.GetResource().GetId(); got != "github" {
		t.Fatalf("resource id = %q, want github", got)
	}
	if got := req.GetAction().GetName(); got != graphQLOperationID {
		t.Fatalf("action name = %q, want %s", got, graphQLOperationID)
	}
}

type brokerOperationConnectionProvider struct {
	*coretesting.StubIntegration
	operationConnections map[string]string
	selector             core.OperationConnectionSelector
	allowOverride        bool
}

type brokerGraphQLProvider struct {
	*coretesting.StubIntegration
	invokeGraphQL func(context.Context, core.GraphQLRequest, string) (*core.OperationResult, error)
}

func (p *brokerGraphQLProvider) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, token string) (*core.OperationResult, error) {
	if p.invokeGraphQL != nil {
		return p.invokeGraphQL(ctx, request, token)
	}
	return &core.OperationResult{Status: 200}, nil
}

type recordingAuthorizationProvider struct {
	allowed         bool
	lastCheckAccess *proto.CheckAccessRequest
}

func (p *recordingAuthorizationProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.lastCheckAccess = req
	return &proto.CheckAccessResponse{Allowed: p.allowed}, nil
}

func (p *recordingAuthorizationProvider) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{}, nil
}

func (p *recordingAuthorizationProvider) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func (p *recordingAuthorizationProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (p *recordingAuthorizationProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *recordingAuthorizationProvider) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}

func (p *recordingAuthorizationProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (p *recordingAuthorizationProvider) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}

func (p *recordingAuthorizationProvider) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}

func (p *recordingAuthorizationProvider) Ping(context.Context) error { return nil }

func (p *recordingAuthorizationProvider) Close() error { return nil }

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
