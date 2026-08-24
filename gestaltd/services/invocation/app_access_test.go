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
	const userID = "user-app-access"
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
