package invocation

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const (
	appAccessDefaultsUserID = "4f1d2e3c-5b6a-47c8-9d0e-1f2a3b4c5d6e"
	appAccessDefaultsApp    = "slack"
)

func appAccessDefaultsPrincipal() *principal.Principal {
	return &principal.Principal{
		SubjectID: principal.UserSubjectID(appAccessDefaultsUserID),
		UserID:    appAccessDefaultsUserID,
		Kind:      principal.KindUser,
	}
}

func newAppAccessDefaultsProvider(operations ...catalog.CatalogOperation) *coretesting.StubIntegration {
	return &coretesting.StubIntegration{
		N:          appAccessDefaultsApp,
		ConnMode:   core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Name: appAccessDefaultsApp, Operations: operations},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: 200}, nil
		},
	}
}

// listedAllowed asks the batch listing path about each operation and returns
// the ones it allows.
func listedAllowed(t *testing.T, broker *Broker, p *principal.Principal, operations ...string) []string {
	t.Helper()
	queries := make([]OperationAccessQuery, len(operations))
	for i, operation := range operations {
		queries[i] = OperationAccessQuery{Provider: appAccessDefaultsApp, Operation: operation}
	}
	decisions, err := broker.CheckOperationAccessMany(context.Background(), p, queries)
	if err != nil {
		t.Fatalf("CheckOperationAccessMany: %v", err)
	}
	var allowed []string
	for i, decision := range decisions {
		switch {
		case decision.Err == nil:
			allowed = append(allowed, operations[i])
		case !errors.Is(decision.Err, ErrAuthorizationDenied):
			t.Fatalf("listing %s error = %v, want nil or ErrAuthorizationDenied", operations[i], decision.Err)
		}
	}
	return allowed
}

func requireInvokeAllowed(t *testing.T, broker *Broker, p *principal.Principal, operation string) {
	t.Helper()
	if _, err := broker.Invoke(context.Background(), p, appAccessDefaultsApp, "", operation, nil); err != nil {
		t.Fatalf("Invoke %s = %v, want allowed", operation, err)
	}
}

func requireInvokeDenied(t *testing.T, broker *Broker, p *principal.Principal, operation string) {
	t.Helper()
	if _, err := broker.Invoke(context.Background(), p, appAccessDefaultsApp, "", operation, nil); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("Invoke %s = %v, want ErrAuthorizationDenied", operation, err)
	}
}

func TestBrokerAppAccessOperationAddedLaterFollowsDefaultsForOptOutUser(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	provider := newAppAccessDefaultsProvider(
		catalog.CatalogOperation{ID: "conversations.list", Method: "GET"},
		catalog.CatalogOperation{ID: "chat.postMessage", Method: "POST"},
	)
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		nil,
		WithAppAccessProfiles(svc.AppAccessProfiles),
	)
	p := appAccessDefaultsPrincipal()
	if _, err := svc.AppAccessProfiles.SetAppAccessOverrides(
		context.Background(), p.SubjectID, appAccessDefaultsApp, []string{"chat.postMessage"}, nil,
	); err != nil {
		t.Fatalf("SetAppAccessOverrides: %v", err)
	}

	provider.CatalogVal.Operations = append(provider.CatalogVal.Operations,
		catalog.CatalogOperation{ID: "files.list", Method: "GET"},
		catalog.CatalogOperation{ID: "chat.delete", Method: "POST"},
	)

	requireInvokeAllowed(t, broker, p, "conversations.list")
	requireInvokeAllowed(t, broker, p, "files.list")
	requireInvokeAllowed(t, broker, p, "chat.delete")
	requireInvokeDenied(t, broker, p, "chat.postMessage")

	got := listedAllowed(t, broker, p, "conversations.list", "chat.postMessage", "files.list", "chat.delete")
	want := []string{"conversations.list", "files.list", "chat.delete"}
	if !slices.Equal(got, want) {
		t.Fatalf("listing allowed = %v, want %v", got, want)
	}
}

func TestBrokerAppAccessEmptyProfileAllowsOperationsAddedLater(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	provider := newAppAccessDefaultsProvider(catalog.CatalogOperation{ID: "conversations.list", Method: "GET"})
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		nil,
		WithAppAccessProfiles(svc.AppAccessProfiles),
	)
	p := appAccessDefaultsPrincipal()
	if _, err := svc.AppAccessProfiles.EnsureAppAccessDefaults(context.Background(), p.SubjectID, appAccessDefaultsApp); err != nil {
		t.Fatalf("EnsureAppAccessDefaults: %v", err)
	}

	provider.CatalogVal.Operations = append(provider.CatalogVal.Operations,
		catalog.CatalogOperation{ID: "files.list", Method: "GET"},
	)

	requireInvokeAllowed(t, broker, p, "files.list")
	if got := listedAllowed(t, broker, p, "files.list"); !slices.Equal(got, []string{"files.list"}) {
		t.Fatalf("listing allowed = %v, want files.list", got)
	}
}

func TestBrokerAppAccessExtraOperationEnablesNonDefaultOperation(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	hidden := false
	provider := newAppAccessDefaultsProvider(
		catalog.CatalogOperation{ID: "conversations.list", Method: "GET"},
		catalog.CatalogOperation{ID: "admin.reset", Method: "POST", Visible: &hidden},
		catalog.CatalogOperation{ID: "admin.purge", Method: "POST", Visible: &hidden},
	)
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		nil,
		WithAppAccessProfiles(svc.AppAccessProfiles),
	)
	p := appAccessDefaultsPrincipal()
	if _, err := svc.AppAccessProfiles.SetAppAccessOverrides(
		context.Background(), p.SubjectID, appAccessDefaultsApp, nil, []string{"admin.reset"},
	); err != nil {
		t.Fatalf("SetAppAccessOverrides: %v", err)
	}

	requireInvokeAllowed(t, broker, p, "admin.reset")
	requireInvokeDenied(t, broker, p, "admin.purge")
	got := listedAllowed(t, broker, p, "conversations.list", "admin.reset", "admin.purge")
	if want := []string{"conversations.list", "admin.reset"}; !slices.Equal(got, want) {
		t.Fatalf("listing allowed = %v, want %v", got, want)
	}
}

type legacyAppAccessStore struct {
	core.AppAccessProfileStore
	profile *core.AppAccessProfile
}

func (s legacyAppAccessStore) GetAppAccessProfile(context.Context, string, string) (*core.AppAccessProfile, error) {
	return s.profile, nil
}

func TestBrokerAppAccessLegacyProfileEnforcesItsOldListExactly(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	hidden := false
	provider := newAppAccessDefaultsProvider(
		catalog.CatalogOperation{ID: "conversations.list", Method: "GET"},
		catalog.CatalogOperation{ID: "chat.postMessage", Method: "POST"},
		catalog.CatalogOperation{ID: "admin.reset", Method: "POST", Visible: &hidden},
	)
	p := appAccessDefaultsPrincipal()
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		nil,
		WithAppAccessProfiles(legacyAppAccessStore{
			AppAccessProfileStore: svc.AppAccessProfiles,
			profile: &core.AppAccessProfile{
				SubjectID:               p.SubjectID,
				App:                     appAccessDefaultsApp,
				Legacy:                  true,
				LegacyEnabledOperations: []string{"conversations.list", "admin.reset"},
			},
		}),
	)

	provider.CatalogVal.Operations = append(provider.CatalogVal.Operations,
		catalog.CatalogOperation{ID: "files.list", Method: "GET"},
	)

	requireInvokeAllowed(t, broker, p, "conversations.list")
	requireInvokeAllowed(t, broker, p, "admin.reset")
	requireInvokeDenied(t, broker, p, "chat.postMessage")
	requireInvokeDenied(t, broker, p, "files.list")

	got := listedAllowed(t, broker, p, "conversations.list", "chat.postMessage", "admin.reset", "files.list")
	if want := []string{"conversations.list", "admin.reset"}; !slices.Equal(got, want) {
		t.Fatalf("listing allowed = %v, want %v", got, want)
	}
}

func TestBrokerAppAccessHonorsAppPublishedDefaults(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	provider := &appAccessDefaultsListedProvider{
		StubIntegration: newAppAccessDefaultsProvider(
			catalog.CatalogOperation{ID: "conversations.list", Method: "GET"},
			catalog.CatalogOperation{ID: "chat.postMessage", Method: "POST"},
		),
		defaults: []string{"conversations.list"},
	}
	broker := NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		nil,
		WithAppAccessProfiles(svc.AppAccessProfiles),
	)
	p := appAccessDefaultsPrincipal()
	if _, err := svc.AppAccessProfiles.EnsureAppAccessDefaults(context.Background(), p.SubjectID, appAccessDefaultsApp); err != nil {
		t.Fatalf("EnsureAppAccessDefaults: %v", err)
	}

	requireInvokeAllowed(t, broker, p, "conversations.list")
	requireInvokeDenied(t, broker, p, "chat.postMessage")
	if got := listedAllowed(t, broker, p, "conversations.list", "chat.postMessage"); !slices.Equal(got, []string{"conversations.list"}) {
		t.Fatalf("listing allowed = %v, want conversations.list", got)
	}
}

type appAccessDefaultsListedProvider struct {
	*coretesting.StubIntegration
	defaults []string
}

func (p *appAccessDefaultsListedProvider) DefaultAppAccessOperations() ([]string, bool) {
	return p.defaults, true
}
