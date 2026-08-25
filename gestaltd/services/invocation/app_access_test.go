package invocation

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestBrokerAppAccessProfileIsSharedByInvocationAndListing(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	provider := &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "slack",
			Operations: []catalog.CatalogOperation{
				{ID: "conversations.list", Method: "GET"},
				{ID: "chat.postMessage", Method: "POST"},
			},
		},
		ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: 200}, nil
		},
	}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		nil,
		WithAppAccessProfiles(svc.AppAccessProfiles),
	)
	const userID = "4f1d2e3c-5b6a-47c8-9d0e-1f2a3b4c5d6e"
	if _, err := svc.AppAccessProfiles.EnsureAppAccessDefaults(
		context.Background(),
		principal.UserSubjectID(userID),
		"slack",
		[]string{"conversations.list"},
	); err != nil {
		t.Fatalf("EnsureAppAccessDefaults: %v", err)
	}
	p := &principal.Principal{
		SubjectID: principal.UserSubjectID(userID),
		UserID:    userID,
		Kind:      principal.KindUser,
		Scopes:    []string{"openid", "email", "profile"},
	}

	if err := broker.CheckOperationAccess(context.Background(), p, "slack", "conversations.list"); err != nil {
		t.Fatalf("read operation access = %v, want allowed", err)
	}
	if err := broker.CheckOperationAccess(context.Background(), p, "slack", "chat.postMessage"); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("write operation access = %v, want ErrAuthorizationDenied", err)
	}
	if _, err := broker.Invoke(context.Background(), p, "slack", "", "chat.postMessage", nil); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("write invoke = %v, want ErrAuthorizationDenied", err)
	}
}

func TestBrokerAppAccessProfileResolvesOpaqueSubjectToCanonicalUser(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	user, err := svc.Users.FindOrCreateUser(context.Background(), "opaque-subject@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	provider := &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{ID: "conversations.list", Method: "GET"},
		}},
	}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		nil,
		WithAppAccessProfiles(svc.AppAccessProfiles),
	)
	if _, err := svc.AppAccessProfiles.EnsureAppAccessDefaults(
		context.Background(),
		principal.UserSubjectID(user.ID),
		"slack",
		[]string{"conversations.list"},
	); err != nil {
		t.Fatalf("EnsureAppAccessDefaults: %v", err)
	}

	p := &principal.Principal{
		SubjectID: principal.UserSubjectID("auth0|opaque-user"),
		Identity:  &core.UserIdentity{Email: user.Email},
		Kind:      principal.KindUser,
		Scopes:    []string{"openid", "email", "profile"},
	}
	if err := broker.CheckOperationAccess(context.Background(), p, "slack", "conversations.list"); err != nil {
		t.Fatalf("opaque subject access = %v, want allowed after canonicalization", err)
	}
	withoutVerifiedIdentity := &principal.Principal{
		SubjectID: principal.UserSubjectID("auth0|opaque-user"),
		Kind:      principal.KindUser,
	}
	if err := broker.CheckOperationAccess(context.Background(), withoutVerifiedIdentity, "slack", "conversations.list"); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("opaque subject without verified identity = %v, want ErrAuthorizationDenied", err)
	}
}

func TestBrokerAppAccessProfileCoversEveryInvocationMode(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	const userID = "4f1d2e3c-5b6a-47c8-9d0e-1f2a3b4c5d6e"
	streamCalled := false
	executeCalled := false
	graphQLCalled := false
	provider := &brokerGraphQLProvider{
		StubIntegration: &coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
				{ID: "chat.postMessage", Method: "POST"},
				{ID: "events.watch", Method: "GET", Response: &catalog.OperationResponseSpec{
					Stream: &catalog.StreamResponseSpec{MediaType: "application/x-ndjson"},
				}},
			}},
			ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
				executeCalled = true
				return &core.OperationResult{Status: 200}, nil
			},
			StreamFn: func(context.Context, string, map[string]any, string) (core.StreamReader, error) {
				streamCalled = true
				return &sliceCoreStreamReader{}, nil
			},
		},
		invokeGraphQL: func(context.Context, core.GraphQLRequest, string) (*core.OperationResult, error) {
			graphQLCalled = true
			return &core.OperationResult{Status: 200}, nil
		},
	}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		nil,
		WithAppAccessProfiles(svc.AppAccessProfiles),
	)
	if _, err := svc.AppAccessProfiles.EnsureAppAccessDefaults(
		context.Background(),
		principal.UserSubjectID(userID),
		"slack",
		[]string{"allowed.operation"},
	); err != nil {
		t.Fatalf("EnsureAppAccessDefaults: %v", err)
	}
	p := &principal.Principal{
		SubjectID: principal.UserSubjectID(userID),
		UserID:    userID,
		Kind:      principal.KindUser,
	}

	assertDenied := func(t *testing.T, mode string, err error) {
		t.Helper()
		if !errors.Is(err, ErrAuthorizationDenied) {
			t.Fatalf("%s error = %v, want ErrAuthorizationDenied", mode, err)
		}
	}
	_, err := broker.Invoke(context.Background(), p, "slack", "", "chat.postMessage", nil)
	assertDenied(t, "unary", err)
	_, err = broker.InvokeStream(context.Background(), p, "slack", "", "events.watch", nil)
	assertDenied(t, "stream", err)
	_, err = broker.InvokeMaybeStream(context.Background(), p, "slack", "", "events.watch", nil)
	assertDenied(t, "maybe stream", err)
	_, err = broker.InvokeGraphQL(context.Background(), p, "slack", "", core.GraphQLRequest{Document: "query { viewer { id } }"})
	assertDenied(t, "graphql", err)
	if executeCalled || streamCalled || graphQLCalled {
		t.Fatal("a disabled operation reached the provider")
	}
}

func TestBrokerAppAccessProfileAllowsGraphQLCapability(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	called := false
	provider := &brokerGraphQLProvider{
		StubIntegration: &coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
				{ID: "conversations.list", Method: "GET"},
			}},
		},
		invokeGraphQL: func(context.Context, core.GraphQLRequest, string) (*core.OperationResult, error) {
			called = true
			return &core.OperationResult{Status: 200}, nil
		},
	}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		nil,
		WithAppAccessProfiles(svc.AppAccessProfiles),
	)
	const userID = "4f1d2e3c-5b6a-47c8-9d0e-1f2a3b4c5d6e"
	if _, err := svc.AppAccessProfiles.EnsureAppAccessDefaults(
		context.Background(),
		principal.UserSubjectID(userID),
		"slack",
		[]string{core.GraphQLCapabilityID},
	); err != nil {
		t.Fatalf("EnsureAppAccessDefaults: %v", err)
	}
	p := &principal.Principal{
		SubjectID: principal.UserSubjectID(userID),
		UserID:    userID,
		Kind:      principal.KindUser,
	}
	if _, err := broker.InvokeGraphQL(context.Background(), p, "slack", "", core.GraphQLRequest{Document: "query { viewer { id } }"}); err != nil {
		t.Fatalf("InvokeGraphQL = %v, want allowed", err)
	}
	if !called {
		t.Fatal("GraphQL provider was not called")
	}
}
