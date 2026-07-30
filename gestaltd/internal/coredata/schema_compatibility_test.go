package coredata_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

// schemaMatchingIndexedDB models relationaldb's CreateObjectStore behavior for
// stores whose schema metadata already exists. relationaldb persists the full
// record blob, so fields that are not part of a key or index remain available.
type schemaMatchingIndexedDB struct {
	inner    indexeddb.IndexedDB
	existing map[string]idb.ObjectStoreOptions
}

func (d *schemaMatchingIndexedDB) ObjectStore(name string) idb.ObjectStore {
	return d.inner.ObjectStore(name)
}

func (d *schemaMatchingIndexedDB) Transaction(
	ctx context.Context,
	stores []string,
	mode idb.TransactionMode,
	opts idb.TransactionOptions,
) (idb.Transaction, error) {
	return d.inner.Transaction(ctx, stores, mode, opts)
}

func (d *schemaMatchingIndexedDB) CreateObjectStore(
	ctx context.Context,
	name string,
	schema idb.ObjectStoreOptions,
) (idb.ObjectStore, error) {
	if existing, ok := d.existing[name]; ok && !reflect.DeepEqual(existing, schema) {
		return nil, fmt.Errorf("object store %q schema does not match", name)
	}
	return d.inner.CreateObjectStore(ctx, name, schema)
}

func (d *schemaMatchingIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	return d.inner.DeleteObjectStore(ctx, name)
}

func (d *schemaMatchingIndexedDB) Ping(ctx context.Context) error { return d.inner.Ping(ctx) }
func (d *schemaMatchingIndexedDB) Close() error                   { return d.inner.Close() }

func TestRuntimeHeartbeatFieldsRoundTripWithLegacyStoreSchemas(t *testing.T) {
	t.Parallel()

	legacySourceVersionSchema := idb.ObjectStoreOptions{
		Columns: []idb.ColumnDef{
			{Name: "id", Type: idb.TypeString, PrimaryKey: true},
			{Name: "current_source_version", Type: idb.TypeString},
			{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
		},
	}
	legacyAppRolloutsSchema := idb.ObjectStoreOptions{
		Indexes: []idb.IndexSchema{
			{Name: "by_state", KeyPath: []string{"state"}},
		},
		Columns: []idb.ColumnDef{
			{Name: "id", Type: idb.TypeString, PrimaryKey: true},
			{Name: "app", Type: idb.TypeString, NotNull: true, Unique: true},
			{Name: "version", Type: idb.TypeString, NotNull: true},
			{Name: "state", Type: idb.TypeString, NotNull: true},
			{Name: "target_source_version", Type: idb.TypeString},
			{Name: "created_at", Type: idb.TypeTime, NotNull: true},
			{Name: "enrollment_ends_at", Type: idb.TypeTime, NotNull: true},
			{Name: "deadline", Type: idb.TypeTime, NotNull: true},
			{Name: "completed_at", Type: idb.TypeTime},
			{Name: "failed_at", Type: idb.TypeTime},
		},
	}
	db := &schemaMatchingIndexedDB{
		inner: &coretesting.StubIndexedDB{},
		existing: map[string]idb.ObjectStoreOptions{
			coredata.StoreGestaltdSourceVersionState: legacySourceVersionSchema,
			coredata.StoreAppRollouts:                legacyAppRolloutsSchema,
		},
	}
	services, err := coredata.New(db)
	if err != nil {
		t.Fatalf("bootstrap with legacy schemas: %v", err)
	}

	ctx := context.Background()
	start := time.Date(2026, 7, 30, 16, 0, 0, 0, time.UTC)
	state, err := services.GestaltdSourceVersionState.ActivateWithRolloutMode(
		ctx,
		"source-a",
		start,
		false,
		time.Minute,
		5*time.Minute,
		core.AppRolloutModeHeartbeat,
		2,
	)
	if err != nil {
		t.Fatalf("activate heartbeat source version: %v", err)
	}
	if state.MinimumHealthyInstances != 2 {
		t.Fatalf("minimum healthy instances = %d, want 2", state.MinimumHealthyInstances)
	}
	state, err = services.GestaltdSourceVersionState.Get(ctx)
	if err != nil {
		t.Fatalf("read source version state: %v", err)
	}
	if state.MinimumHealthyInstances != 2 {
		t.Fatalf("round-tripped minimum healthy instances = %d, want 2", state.MinimumHealthyInstances)
	}

	rollout, err := services.GestaltdSourceVersionState.CreateAppRollout(ctx, &core.AppRollout{
		App:              "g-issues",
		Version:          "v2",
		State:            core.AppRolloutStateRestarting,
		Mode:             core.AppRolloutModeHeartbeat,
		CreatedAt:        start,
		EnrollmentEndsAt: start.Add(time.Minute),
		Deadline:         start.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create heartbeat rollout: %v", err)
	}
	evaluatedAt := start.Add(time.Minute)
	rollout, transitioned, err := services.AppRollouts.EvaluateHeartbeatRollout(
		ctx,
		rollout,
		coredata.HeartbeatRolloutEvaluation{
			Healthy:         true,
			StabilityWindow: 2 * time.Minute,
			EvaluatedAt:     evaluatedAt,
		},
	)
	if err != nil {
		t.Fatalf("evaluate healthy heartbeat: %v", err)
	}
	if transitioned {
		t.Fatal("healthy heartbeat transitioned before the stability window")
	}
	rollout, err = services.AppRollouts.Get(ctx, rollout.App)
	if err != nil {
		t.Fatalf("read healthy heartbeat rollout: %v", err)
	}
	if rollout.Mode != core.AppRolloutModeHeartbeat ||
		rollout.TargetSourceVersion != "source-a" ||
		rollout.MinimumHealthyInstances != 2 ||
		!rollout.HealthySince.Equal(evaluatedAt) ||
		!rollout.HeartbeatEvaluatedAt.Equal(evaluatedAt) {
		t.Fatalf("round-tripped heartbeat rollout = %#v", rollout)
	}

	failedAt := start.Add(5 * time.Minute)
	_, transitioned, err = services.AppRollouts.EvaluateHeartbeatRollout(
		ctx,
		rollout,
		coredata.HeartbeatRolloutEvaluation{
			Healthy:         false,
			StabilityWindow: 2 * time.Minute,
			EvaluatedAt:     failedAt,
			FailureSummary: core.AppRolloutFailureSummary{
				LiveInstances:         1,
				RunningDesiredVersion: 0,
				Mismatched:            1,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate failed heartbeat: %v", err)
	}
	if !transitioned {
		t.Fatal("deadline heartbeat did not transition")
	}

	// A second bootstrap represents either a replacement process or an older
	// process joining during a rolling deployment. It must accept the unchanged
	// metadata and preserve fields that only exist in the record blob.
	services, err = coredata.New(db)
	if err != nil {
		t.Fatalf("repeat bootstrap with legacy schemas: %v", err)
	}
	rollout, err = services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("read failed heartbeat rollout after repeat bootstrap: %v", err)
	}
	if rollout.State != core.AppRolloutStateFailed ||
		!rollout.FailedAt.Equal(failedAt) ||
		!rollout.HeartbeatEvaluatedAt.Equal(failedAt) ||
		rollout.FailureSummary == nil ||
		rollout.FailureSummary.LiveInstances != 1 ||
		rollout.FailureSummary.MinimumHealthyInstances != 2 ||
		rollout.FailureSummary.SourceVersion != "source-a" {
		t.Fatalf("round-tripped failed heartbeat rollout = %#v", rollout)
	}
}
