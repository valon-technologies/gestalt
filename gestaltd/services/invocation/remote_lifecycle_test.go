package invocation

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/remotetest"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestPlan6BrokerRemoteClientSetCarriesBearerToken(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	clients, err := fake.NewClientSet(context.Background())
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	defer func() { _ = clients.Close() }()

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t),
		svc.Users,
		svc.ExternalCredentials,
		WithRemoteAppRouting(
			func(name string) bool { return name == "linear" },
			clients.App,
			"http://127.0.0.1:8080",
			testRemoteRequestContext,
		),
	)

	_, err = broker.Invoke(context.Background(), &principal.Principal{
		SubjectID: "user:test",
		Kind:      principal.KindUser,
		Scopes:    []string{"linear"},
	}, "linear", "", "issues.list", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	appInvokes := fake.Recorder.AppInvokesSnapshot()
	if len(appInvokes) != 1 {
		t.Fatalf("remote invokes = %d, want 1", len(appInvokes))
	}
	if appInvokes[0].Authorization != "Bearer "+fake.Token {
		t.Fatalf("authorization = %q", appInvokes[0].Authorization)
	}
}

func TestPlan6BrokerLocalWinsWhenRegistered(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	clients, err := fake.NewClientSet(context.Background())
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	defer func() { _ = clients.Close() }()

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Operations: []catalog.CatalogOperation{{ID: "issues.list"}},
			},
			ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: 202, Body: []byte("local")}, nil
			},
		}),
		svc.Users,
		svc.ExternalCredentials,
		WithRemoteAppRouting(
			func(name string) bool { return name == "linear" },
			clients.App,
			"http://127.0.0.1:8080",
			testRemoteRequestContext,
		),
	)

	result, err := broker.Invoke(context.Background(), &principal.Principal{
		SubjectID: "user:test",
		Kind:      principal.KindUser,
		Scopes:    []string{"linear"},
	}, "linear", "", "issues.list", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result == nil || result.Status != 202 {
		t.Fatalf("result = %#v, want local 202", result)
	}
	if len(fake.Recorder.AppInvokesSnapshot()) != 0 {
		t.Fatalf("remote invokes = %d, want 0", len(fake.Recorder.AppInvokesSnapshot()))
	}
}
