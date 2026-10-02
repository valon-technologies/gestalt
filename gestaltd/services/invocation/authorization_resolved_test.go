package invocation

import (
	"context"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

const (
	sessionOperationID         = "session.op"
	graphQLCapabilityOperation = core.GraphQLCapabilityID
)

func sessionOperationAllowed(t *testing.T, broker *Broker, metadata *catalog.CatalogOperation) bool {
	t.Helper()
	decisions, err := broker.CheckOperationAccessMany(
		context.Background(),
		appAccessDefaultsPrincipal(),
		[]OperationAccessQuery{{Provider: appAccessDefaultsApp, Operation: sessionOperationID, Metadata: metadata}},
	)
	if err != nil {
		t.Fatalf("CheckOperationAccessMany: %v", err)
	}
	return decisions[0].Err == nil
}

func TestCheckOperationAccessManyJudgesTheResolvedOperation(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	hidden := false
	provider := newAppAccessDefaultsProvider(catalog.CatalogOperation{ID: sessionOperationID, Method: "GET"})
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

	sessionHidden := &catalog.CatalogOperation{ID: sessionOperationID, Method: "GET", Visible: &hidden}
	if sessionOperationAllowed(t, broker, sessionHidden) {
		t.Fatal("hidden session operation without an Extra entry was listed")
	}
	if !sessionOperationAllowed(t, broker, nil) {
		t.Fatal("nil Metadata did not fall back to the static catalog")
	}

	if _, err := svc.AppAccessProfiles.SetAppAccessOverrides(
		context.Background(), p.SubjectID, appAccessDefaultsApp, nil, []string{sessionOperationID},
	); err != nil {
		t.Fatalf("SetAppAccessOverrides: %v", err)
	}
	if !sessionOperationAllowed(t, broker, sessionHidden) {
		t.Fatal("hidden session operation with an Extra entry was denied")
	}
}

func TestFilterCatalogForPrincipalUsesSessionOperations(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	hidden := false
	provider := newAppAccessDefaultsProvider(
		catalog.CatalogOperation{ID: "conversations.list", Method: "GET"},
		catalog.CatalogOperation{ID: sessionOperationID, Method: "GET"},
	)
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

	session := &catalog.Catalog{Name: appAccessDefaultsApp, Operations: []catalog.CatalogOperation{
		{ID: "conversations.list", Method: "GET"},
		{ID: sessionOperationID, Method: "GET", Visible: &hidden},
		{ID: graphQLCapabilityOperation, Method: "POST"},
	}}
	filtered, err := FilterCatalogForPrincipal(context.Background(), session, appAccessDefaultsApp, p, broker)
	if err != nil {
		t.Fatalf("FilterCatalogForPrincipal: %v", err)
	}
	var got []string
	for _, op := range filtered.Operations {
		got = append(got, op.ID)
	}
	want := []string{"conversations.list", graphQLCapabilityOperation}
	if !slices.Equal(got, want) {
		t.Fatalf("listed = %v, want %v", got, want)
	}
}
