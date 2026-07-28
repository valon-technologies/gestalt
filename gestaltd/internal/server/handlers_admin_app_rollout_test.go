package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
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
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = services
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-empty":      {Source: config.ProviderSource{Registry: "toolshed"}},
			"g-issues":     {Source: config.ProviderSource{Registry: "toolshed"}},
			"snapshot-app": {},
		}
	})
	testutil.CloseOnCleanup(t, ts)
	return ts
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
