package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAdminRegistryApps(t *testing.T) {
	t.Parallel()

	t.Run("lists registry managed apps", func(t *testing.T) {
		t.Parallel()
		services := testutil.NewStubServices(t)
		start := time.Now().UTC().Truncate(time.Second)
		appendKnownVersion(t, services, "g-issues", "1.2.3", start)
		rollout := createRollout(t, services, "g-issues", "1.2.3", start)
		acknowledgeMaterialization(t, services, "replica-a", rollout, start.Add(10*time.Second), true)
		acknowledgeMaterializationWithSource(t, services, "replica-old", "source-old", rollout, start.Add(10*time.Second), false)
		acknowledgeMaterialization(t, services, "replica-late", rollout, rollout.EnrollmentEndsAt.Add(time.Second), false)

		ts := newRegistryObservabilityTestServer(t, services)
		resp, err := http.Get(ts.URL + "/admin/api/v1/registry-apps")
		if err != nil {
			t.Fatalf("GET registry apps: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d: %s", resp.StatusCode, body)
		}
		var payload []struct {
			App            string `json:"app"`
			Registry       string `json:"registry"`
			DesiredVersion string `json:"desiredVersion"`
			Rollout        struct {
				State               string `json:"state"`
				TargetSourceVersion string `json:"targetSourceVersion"`
			} `json:"rollout"`
			Cohort struct {
				Acknowledged int `json:"acknowledged"`
				Materialized int `json:"materialized"`
				Restarted    int `json:"restarted"`
			} `json:"cohort"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode registry apps: %v", err)
		}
		if len(payload) != 2 {
			t.Fatalf("registry apps = %#v, want two configured registry-only apps", payload)
		}
		if payload[0].App != "g-empty" || payload[0].DesiredVersion != "" {
			t.Fatalf("empty registry app = %#v", payload[0])
		}
		got := payload[1]
		if got.App != "g-issues" || got.Registry != "toolshed" || got.DesiredVersion != "1.2.3" {
			t.Fatalf("registry app = %#v", got)
		}
		if got.Rollout.State != string(core.AppRolloutStateEnrolling) {
			t.Fatalf("rollout state = %q", got.Rollout.State)
		}
		if got.Rollout.TargetSourceVersion != "source-target" {
			t.Fatalf("target source version = %q", got.Rollout.TargetSourceVersion)
		}
		if got.Cohort.Acknowledged != 1 || got.Cohort.Materialized != 1 || got.Cohort.Restarted != 1 {
			t.Fatalf("cohort = %#v, want only on-time replica counted", got.Cohort)
		}
	})

	t.Run("merges desired version and rollout", func(t *testing.T) {
		t.Parallel()
		services := testutil.NewStubServices(t)
		start := time.Now().UTC().Truncate(time.Second)
		appendKnownVersion(t, services, "g-issues", "1.2.3", start)
		createRollout(t, services, "g-issues", "1.2.3", start)
		ts := newRegistryObservabilityTestServer(t, services)

		resp, err := http.Get(ts.URL + "/admin/api/v1/registry-apps/g-issues")
		if err != nil {
			t.Fatalf("GET registry app: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d: %s", resp.StatusCode, body)
		}
		var payload struct {
			App            string                     `json:"app"`
			DesiredVersion string                     `json:"desiredVersion"`
			KnownVersions  []adminAppInstallationJSON `json:"knownVersions"`
			Rollout        struct {
				Version             string `json:"version"`
				State               string `json:"state"`
				TargetSourceVersion string `json:"targetSourceVersion"`
			} `json:"rollout"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode registry app: %v", err)
		}
		if payload.App != "g-issues" || payload.DesiredVersion != "1.2.3" || payload.Rollout.Version != "1.2.3" ||
			payload.Rollout.State != "enrolling" || payload.Rollout.TargetSourceVersion != "source-target" {
			t.Fatalf("registry app = %#v", payload)
		}
		if len(payload.KnownVersions) != 1 || payload.KnownVersions[0].Version != "1.2.3" {
			t.Fatalf("known versions = %#v", payload.KnownVersions)
		}
	})

	t.Run("rejects non registry app detail", func(t *testing.T) {
		t.Parallel()
		services := testutil.NewStubServices(t)
		ts := newRegistryObservabilityTestServer(t, services)
		resp, err := http.Get(ts.URL + "/admin/api/v1/registry-apps/snapshot-app")
		if err != nil {
			t.Fatalf("GET snapshot app: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("excludes progress from an earlier rollout of the same version", func(t *testing.T) {
		t.Parallel()
		services := testutil.NewStubServices(t)
		start := time.Now().UTC().Truncate(time.Second)
		appendKnownVersion(t, services, "g-issues", "1.2.3", start)
		stale := &core.AppRollout{
			App:     "g-issues",
			Version: "1.2.3",
		}
		acknowledgeMaterialization(t, services, "replica-stale", stale, start.Add(-time.Hour), true)
		createRollout(t, services, "g-issues", "1.2.3", start)
		ts := newRegistryObservabilityTestServer(t, services)

		resp, err := http.Get(ts.URL + "/admin/api/v1/registry-apps/g-issues")
		if err != nil {
			t.Fatalf("GET registry app: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var payload struct {
			Cohort adminRolloutCohortJSON `json:"cohort"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode registry app: %v", err)
		}
		if payload.Cohort != (adminRolloutCohortJSON{}) {
			t.Fatalf("cohort = %#v, want stale progress excluded", payload.Cohort)
		}

		resp, err = http.Get(ts.URL + "/admin/api/v1/app-rollouts/g-issues/materializations")
		if err != nil {
			t.Fatalf("GET materializations: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var materializations struct {
			Rows []struct {
				InCohort  bool `json:"inCohort"`
				Converged bool `json:"converged"`
			} `json:"materializations"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&materializations); err != nil {
			t.Fatalf("decode materializations: %v", err)
		}
		if len(materializations.Rows) != 1 || materializations.Rows[0].InCohort || materializations.Rows[0].Converged {
			t.Fatalf("materializations = %#v, want stale row outside current rollout", materializations.Rows)
		}
	})
}

func TestAdminRegistryAppFleetObservability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 16, 0, 0, 0, time.UTC)

	t.Run("keeps failed rollout separate from healthy fleet and classifies replicas", func(t *testing.T) {
		t.Parallel()
		services := testutil.NewStubServices(t)
		appendKnownVersion(t, services, "g-issues", "1.2.3", now.Add(-time.Hour))
		if _, err := services.GestaltdSourceVersionState.Activate(
			context.Background(), "source-target", now.Add(-time.Hour), false, time.Minute, 15*time.Minute, 2,
		); err != nil {
			t.Fatalf("Activate: %v", err)
		}
		rollout := createRollout(t, services, "g-issues", "1.2.3", now.Add(-30*time.Minute))
		if _, err := services.AppRollouts.MarkFailed(context.Background(), rollout.App, rollout.Version, now.Add(-15*time.Minute)); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
		upsertFleetHeartbeat(t, services, "current-a", "source-target", now.Add(-5*time.Second), "g-issues", core.GestaltdInstanceAppHeartbeat{
			State: core.GestaltdInstanceAppStateRunning, DesiredVersion: "1.2.3", RunningVersion: "1.2.3", ObservedAt: now.Add(-5 * time.Second),
		})
		upsertFleetHeartbeat(t, services, "current-b", "source-target", now.Add(-10*time.Second), "g-issues", core.GestaltdInstanceAppHeartbeat{
			State: core.GestaltdInstanceAppStateRunning, DesiredVersion: "1.2.3", RunningVersion: "1.2.3", ObservedAt: now.Add(-10 * time.Second),
		})
		upsertFleetHeartbeat(t, services, "current-stale", "source-target", now.Add(-time.Minute), "g-issues", core.GestaltdInstanceAppHeartbeat{
			State: core.GestaltdInstanceAppStateRunning, RunningVersion: "1.2.3", ObservedAt: now.Add(-time.Minute),
		})
		upsertFleetHeartbeat(t, services, "superseded-fresh", "source-old", now.Add(-3*time.Second), "g-issues", core.GestaltdInstanceAppHeartbeat{
			State: core.GestaltdInstanceAppStateError, LastError: "old revision error", ObservedAt: now.Add(-3 * time.Second),
		})

		ts := newRegistryObservabilityTestServerAt(t, services, now)
		resp, err := http.Get(ts.URL + "/admin/api/v1/registry-apps/g-issues")
		if err != nil {
			t.Fatalf("GET registry app: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d: %s", resp.StatusCode, body)
		}
		var payload struct {
			Rollout struct {
				State string `json:"state"`
			} `json:"rollout"`
			FleetState adminFleetStateJSON     `json:"fleetState"`
			Fresh      []adminFleetReplicaJSON `json:"freshReplicas"`
			Stale      []adminFleetReplicaJSON `json:"staleReplicas"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode registry app: %v", err)
		}
		if payload.Rollout.State != "failed" || payload.FleetState.State != "healthy" {
			t.Fatalf("rollout/fleet = %q/%q, want failed/healthy", payload.Rollout.State, payload.FleetState.State)
		}
		if payload.FleetState.LiveInstances != 2 || payload.FleetState.RunningDesiredVersion != 2 ||
			payload.FleetState.MinimumHealthyInstances != 2 || payload.FleetState.SourceVersion != "source-target" {
			t.Fatalf("fleet state = %#v", payload.FleetState)
		}
		if len(payload.Fresh) != 3 || len(payload.Stale) != 1 {
			t.Fatalf("fresh/stale replicas = %#v / %#v", payload.Fresh, payload.Stale)
		}
		if payload.Fresh[2].InstanceID != "superseded-fresh" || payload.Fresh[2].CurrentSource {
			t.Fatalf("superseded replica = %#v", payload.Fresh[2])
		}
		if payload.Stale[0].InstanceID != "current-stale" || payload.Stale[0].Fresh || !payload.Stale[0].CurrentSource {
			t.Fatalf("stale current-source replica = %#v", payload.Stale[0])
		}
	})

	for _, tc := range []struct {
		name        string
		minimum     int
		observation core.GestaltdInstanceAppHeartbeat
		wantState   string
		wantLive    int
		wantErrors  int
	}{
		{
			name: "degraded runtime error", minimum: 1,
			observation: core.GestaltdInstanceAppHeartbeat{State: core.GestaltdInstanceAppStateError, LastError: "provider unavailable", ObservedAt: now},
			wantState:   "degraded", wantLive: 1, wantErrors: 1,
		},
		{
			name: "insufficient capacity is unknown", minimum: 2,
			observation: core.GestaltdInstanceAppHeartbeat{State: core.GestaltdInstanceAppStateRunning, RunningVersion: "1.2.3", ObservedAt: now},
			wantState:   "unknown", wantLive: 1,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			services := testutil.NewStubServices(t)
			appendKnownVersion(t, services, "g-issues", "1.2.3", now.Add(-time.Hour))
			if _, err := services.GestaltdSourceVersionState.Activate(
				context.Background(), "source-target", now.Add(-time.Hour), false, time.Minute, 15*time.Minute, tc.minimum,
			); err != nil {
				t.Fatalf("Activate: %v", err)
			}
			upsertFleetHeartbeat(t, services, "replica", "source-target", now, "g-issues", tc.observation)
			got := getAdminFleetState(t, newRegistryObservabilityTestServerAt(t, services, now))
			if got.State != tc.wantState || got.LiveInstances != tc.wantLive || got.Errors != tc.wantErrors {
				t.Fatalf("fleet state = %#v", got)
			}
		})
	}

	t.Run("missing source and minimum remains unknown", func(t *testing.T) {
		t.Parallel()
		services := testutil.NewStubServices(t)
		appendKnownVersion(t, services, "g-issues", "1.2.3", now.Add(-time.Hour))
		got := getAdminFleetState(t, newRegistryObservabilityTestServerAt(t, services, now))
		if got.State != "unknown" || got.SourceVersion != "" || got.MinimumHealthyInstances != 0 || got.LiveInstances != 0 {
			t.Fatalf("fleet state = %#v", got)
		}
	})

	t.Run("storage read failure returns an error instead of healthy", func(t *testing.T) {
		t.Parallel()
		db := &coretesting.StubIndexedDB{}
		services, err := coredata.New(db)
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		testutil.AttachStubExternalCredentials(services)
		ts := newRegistryObservabilityTestServerAt(t, services, now)
		db.Err = errors.New("indexeddb unavailable")
		resp, err := http.Get(ts.URL + "/admin/api/v1/registry-apps/g-issues")
		if err != nil {
			t.Fatalf("GET registry app: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusInternalServerError {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 500: %s", resp.StatusCode, body)
		}
	})
}

type adminFleetStateJSON struct {
	State                   string `json:"state"`
	SourceVersion           string `json:"sourceVersion"`
	MinimumHealthyInstances int    `json:"minimumHealthyInstances"`
	LiveInstances           int    `json:"liveInstances"`
	RunningDesiredVersion   int    `json:"runningDesiredVersion"`
	Errors                  int    `json:"errors"`
}

type adminFleetReplicaJSON struct {
	InstanceID    string `json:"instanceId"`
	CurrentSource bool   `json:"currentSource"`
	Fresh         bool   `json:"fresh"`
}

func getAdminFleetState(t *testing.T, ts *httptest.Server) adminFleetStateJSON {
	t.Helper()
	resp, err := http.Get(ts.URL + "/admin/api/v1/registry-apps/g-issues")
	if err != nil {
		t.Fatalf("GET registry app: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var payload struct {
		FleetState adminFleetStateJSON `json:"fleetState"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode registry app: %v", err)
	}
	return payload.FleetState
}

type adminAppInstallationJSON struct {
	Version string `json:"version"`
}

type adminRolloutCohortJSON struct {
	Acknowledged int `json:"acknowledged"`
	Materialized int `json:"materialized"`
	Restarted    int `json:"restarted"`
	Failed       int `json:"failed"`
}

func TestAdminAppRollouts(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	start := time.Now().UTC().Truncate(time.Second)
	createRollout(t, services, "g-issues", "1.2.3", start)
	ts := newRegistryObservabilityTestServer(t, services)

	resp, err := http.Get(ts.URL + "/admin/api/v1/app-rollouts?app=g-issues&state=enrolling")
	if err != nil {
		t.Fatalf("GET app rollouts: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var payload []struct {
		App                 string `json:"app"`
		Version             string `json:"version"`
		State               string `json:"state"`
		TargetSourceVersion string `json:"targetSourceVersion"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode app rollouts: %v", err)
	}
	if len(payload) != 1 || payload[0].App != "g-issues" || payload[0].Version != "1.2.3" ||
		payload[0].State != "enrolling" || payload[0].TargetSourceVersion != "source-target" {
		t.Fatalf("rollouts = %#v", payload)
	}
}

func TestAdminAppRolloutMaterializations(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	start := time.Now().UTC().Truncate(time.Second)
	appendKnownVersion(t, services, "g-issues", "1.2.3", start)
	rollout := createRollout(t, services, "g-issues", "1.2.3", start)
	acknowledgeMaterialization(t, services, "replica-b", rollout, start.Add(20*time.Second), true)
	acknowledgeMaterialization(t, services, "replica-a", rollout, rollout.EnrollmentEndsAt.Add(time.Second), false)
	ts := newRegistryObservabilityTestServer(t, services)

	resp, err := http.Get(ts.URL + "/admin/api/v1/app-rollouts/g-issues/materializations")
	if err != nil {
		t.Fatalf("GET materializations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var payload struct {
		App              string `json:"app"`
		Version          string `json:"version"`
		RolloutState     string `json:"rolloutState"`
		Materializations []struct {
			InstanceID    string  `json:"instanceId"`
			SourceVersion string  `json:"sourceVersion"`
			RestartedAt   *string `json:"restartedAt"`
			InCohort      bool    `json:"inCohort"`
			Converged     bool    `json:"converged"`
			AttemptCount  int     `json:"attemptCount"`
		} `json:"materializations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode materializations: %v", err)
	}
	if payload.App != "g-issues" || payload.Version != "1.2.3" || payload.RolloutState != "enrolling" {
		t.Fatalf("payload = %#v", payload)
	}
	if len(payload.Materializations) != 2 || payload.Materializations[0].InstanceID != "replica-a" || payload.Materializations[1].InstanceID != "replica-b" {
		t.Fatalf("materializations = %#v, want instance-sorted rows", payload.Materializations)
	}
	if payload.Materializations[0].InCohort || payload.Materializations[0].Converged {
		t.Fatalf("late replica = %#v, want out of cohort and not converged", payload.Materializations[0])
	}
	if payload.Materializations[0].SourceVersion != "source-target" || payload.Materializations[1].SourceVersion != "source-target" {
		t.Fatalf("source versions = %#v", payload.Materializations)
	}
	if !payload.Materializations[1].InCohort || !payload.Materializations[1].Converged || payload.Materializations[1].RestartedAt == nil {
		t.Fatalf("on-time replica = %#v, want converged cohort member", payload.Materializations[1])
	}
}

func newRegistryObservabilityTestServer(t *testing.T, services *coredata.Services) *httptest.Server {
	t.Helper()
	return newRegistryObservabilityTestServerWithClock(t, services, time.Now)
}

func newRegistryObservabilityTestServerAt(t *testing.T, services *coredata.Services, now time.Time) *httptest.Server {
	t.Helper()
	return newRegistryObservabilityTestServerWithClock(t, services, func() time.Time { return now })
}

func newRegistryObservabilityTestServerWithClock(t *testing.T, services *coredata.Services, now func() time.Time) *httptest.Server {
	t.Helper()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = services
		cfg.Now = now
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-empty":      {Source: config.ProviderSource{Registry: "toolshed"}},
			"g-issues":     {Source: config.ProviderSource{Registry: "toolshed"}},
			"snapshot-app": {},
		}
	})
	testutil.CloseOnCleanup(t, ts)
	return ts
}

func upsertFleetHeartbeat(
	t *testing.T,
	services *coredata.Services,
	instanceID string,
	sourceVersion string,
	heartbeatAt time.Time,
	app string,
	observation core.GestaltdInstanceAppHeartbeat,
) {
	t.Helper()
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = heartbeatAt
	}
	if _, err := services.GestaltdInstanceHeartbeats.Upsert(context.Background(), &core.GestaltdInstanceHeartbeat{
		InstanceID:    instanceID,
		SourceVersion: sourceVersion,
		StartedAt:     heartbeatAt.Add(-time.Hour),
		HeartbeatAt:   heartbeatAt,
		Apps: map[string]core.GestaltdInstanceAppHeartbeat{
			app: observation,
		},
	}); err != nil {
		t.Fatalf("Upsert heartbeat: %v", err)
	}
}

func appendKnownVersion(t *testing.T, services *coredata.Services, app, version string, at time.Time) {
	t.Helper()
	_, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         app,
		FromVersion: "0.0.0-config",
		ToVersion:   version,
		Actor:       "user:alice",
		Timestamp:   at,
	})
	if err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}
}

func createRollout(t *testing.T, services *coredata.Services, app, version string, start time.Time) *core.AppRollout {
	t.Helper()
	rollout, err := services.AppRollouts.Create(context.Background(), &core.AppRollout{
		App:                 app,
		Version:             version,
		State:               core.AppRolloutStateEnrolling,
		TargetSourceVersion: "source-target",
		CreatedAt:           start,
		EnrollmentEndsAt:    start.Add(time.Minute),
		Deadline:            start.Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	return rollout
}

func acknowledgeMaterialization(t *testing.T, services *coredata.Services, instanceID string, rollout *core.AppRollout, acknowledgedAt time.Time, converged bool) {
	t.Helper()
	acknowledgeMaterializationWithSource(t, services, instanceID, rollout.TargetSourceVersion, rollout, acknowledgedAt, converged)
}

func acknowledgeMaterializationWithSource(t *testing.T, services *coredata.Services, instanceID, sourceVersion string, rollout *core.AppRollout, acknowledgedAt time.Time, converged bool) {
	t.Helper()
	ctx := context.Background()
	if _, err := services.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID:     instanceID,
		SourceVersion:  sourceVersion,
		App:            rollout.App,
		Version:        rollout.Version,
		AcknowledgedAt: acknowledgedAt,
	}); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if !converged {
		return
	}
	if _, err := services.AppInstanceMaterializations.MarkMaterialized(ctx, instanceID, rollout.App, rollout.Version, acknowledgedAt.Add(time.Second)); err != nil {
		t.Fatalf("MarkMaterialized: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.MarkStopped(ctx, instanceID, rollout.App, rollout.Version, acknowledgedAt.Add(2*time.Second)); err != nil {
		t.Fatalf("MarkStopped: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.MarkRestarted(ctx, instanceID, rollout.App, rollout.Version, acknowledgedAt.Add(3*time.Second)); err != nil {
		t.Fatalf("MarkRestarted: %v", err)
	}
}
