package metricutil_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
)

func TestWrapDatastoreEmitsMethodMetrics(t *testing.T) {
	t.Parallel()

	reader := metrictest.UseManualMeterProvider(t)
	store := metricutil.WrapDatastore(metrictest.NewNamedStubDatastore("wrapped-store", coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "u1", Email: email}, nil
		},
		GetUserFn: func(context.Context, string) (*core.User, error) {
			return nil, errors.New("boom")
		},
		TokenFn: func(context.Context, string, string, string, string) (*core.IntegrationToken, error) {
			return nil, core.ErrNotFound
		},
	}))

	if _, err := store.FindOrCreateUser(context.Background(), "metrics@example.com"); err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	if _, err := store.GetUser(context.Background(), "u1"); err == nil {
		t.Fatal("GetUser: expected error")
	}
	if _, err := store.Token(context.Background(), "u1", "provider", "default", ""); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("Token: got %v, want ErrNotFound", err)
	}

	rm := metrictest.CollectMetrics(t, reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.datastore.count", 1, map[string]string{
		"gestalt.provider": "wrapped-store",
		"gestalt.method":   "find_or_create_user",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.datastore.count", 1, map[string]string{
		"gestalt.provider": "wrapped-store",
		"gestalt.method":   "get_user",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.datastore.error_count", 1, map[string]string{
		"gestalt.provider": "wrapped-store",
		"gestalt.method":   "get_user",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.datastore.count", 1, map[string]string{
		"gestalt.provider": "wrapped-store",
		"gestalt.method":   "token",
	})
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.datastore.error_count", map[string]string{
		"gestalt.provider": "wrapped-store",
		"gestalt.method":   "token",
	})
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.datastore.duration", map[string]string{
		"gestalt.provider": "wrapped-store",
		"gestalt.method":   "find_or_create_user",
	})
}
