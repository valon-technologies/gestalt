package autodeploy

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestControllerDetectsAndAdmitsNewestVersion(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	enableAutoDeploy(t, services, "g-issues")
	reader := &fakeReader{results: []*appregistry.AppIndexFetchResult{
		{Index: testIndex("g-issues", "v2"), ETag: `"v2"`},
		{NotModified: true},
	}}
	installer := &fakeInstaller{}
	controller := testController(services, reader, installer)

	if err := controller.Reconcile(t.Context(), "g-issues"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(installer.inputs) != 1 {
		t.Fatalf("install calls = %d, want 1", len(installer.inputs))
	}
	if got := installer.inputs[0]; got.Registry != "toolshed" || got.App != "g-issues" ||
		got.Version != "v2" || got.Actor != Actor {
		t.Fatalf("install input = %#v", got)
	}
	settings, err := services.AutoDeploySettings.Get(t.Context(), "g-issues")
	if err != nil {
		t.Fatalf("Get settings: %v", err)
	}
	if settings.PendingVersion != "" || settings.LastSeenVersion != "v2" || settings.LastError != "" {
		t.Fatalf("settings = %#v", settings)
	}

	if err := controller.Reconcile(t.Context(), "g-issues"); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if len(installer.inputs) != 1 {
		t.Fatalf("install calls after 304 = %d, want 1", len(installer.inputs))
	}
	if len(reader.ifNoneMatch) != 2 || reader.ifNoneMatch[1] != `"v2"` {
		t.Fatalf("If-None-Match values = %#v", reader.ifNoneMatch)
	}
}

func TestControllerCoalescesDuringActiveRollout(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	enableAutoDeploy(t, services, "g-issues")
	start := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	rollout, err := services.AppRollouts.Create(t.Context(), &core.AppRollout{
		App:              "g-issues",
		Version:          "v1",
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        start,
		EnrollmentEndsAt: start.Add(time.Minute),
		Deadline:         start.Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	reader := &fakeReader{results: []*appregistry.AppIndexFetchResult{
		{Index: testIndex("g-issues", "v2"), ETag: `"v2"`},
		{NotModified: true},
	}}
	installer := &fakeInstaller{}
	controller := testController(services, reader, installer)

	if err := controller.Reconcile(t.Context(), "g-issues"); err != nil {
		t.Fatalf("Reconcile active: %v", err)
	}
	settings, err := services.AutoDeploySettings.Get(t.Context(), "g-issues")
	if err != nil {
		t.Fatalf("Get settings: %v", err)
	}
	if settings.PendingVersion != "v2" || len(installer.inputs) != 0 {
		t.Fatalf("active state = %#v, install calls = %d", settings, len(installer.inputs))
	}

	if _, err := services.AppRollouts.MarkCompleteForRollout(t.Context(), rollout, start.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}
	if err := controller.Reconcile(t.Context(), "g-issues"); err != nil {
		t.Fatalf("Reconcile complete: %v", err)
	}
	if len(installer.inputs) != 1 || installer.inputs[0].Version != "v2" {
		t.Fatalf("install inputs = %#v", installer.inputs)
	}
}

func TestControllerDisablesOnFailedRollout(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	enableAutoDeploy(t, services, "g-issues")
	start := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	rollout, err := services.AppRollouts.Create(t.Context(), &core.AppRollout{
		App:              "g-issues",
		Version:          "v1",
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        start,
		EnrollmentEndsAt: start.Add(time.Minute),
		Deadline:         start.Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	if _, err := services.AppRollouts.MarkFailedForRollout(t.Context(), rollout, start.Add(15*time.Minute)); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	controller := testController(
		services,
		&fakeReader{results: []*appregistry.AppIndexFetchResult{{Index: testIndex("g-issues", "v2")}}},
		&fakeInstaller{},
	)

	if err := controller.Reconcile(t.Context(), "g-issues"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	settings, err := services.AutoDeploySettings.Get(t.Context(), "g-issues")
	if err != nil {
		t.Fatalf("Get settings: %v", err)
	}
	if settings.Enabled || settings.PendingVersion != "" || settings.LastError == "" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestControllerCandidateRejectionWaitsForNextPublish(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	enableAutoDeploy(t, services, "g-issues")
	reader := &fakeReader{results: []*appregistry.AppIndexFetchResult{
		{Index: testIndex("g-issues", "v2"), ETag: `"v2"`},
		{Index: testIndex("g-issues", "v2"), ETag: `"v2"`},
		{Index: testIndex("g-issues", "v3"), ETag: `"v3"`},
	}}
	installer := &fakeInstaller{errs: []error{
		appregistry.ErrInstallValidationFailed,
		nil,
	}}
	controller := testController(services, reader, installer)

	if err := controller.Reconcile(t.Context(), "g-issues"); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	settings, err := services.AutoDeploySettings.Get(t.Context(), "g-issues")
	if err != nil {
		t.Fatalf("Get settings: %v", err)
	}
	if settings.PendingVersion != "" || settings.LastError == "" || len(installer.inputs) != 1 {
		t.Fatalf("rejected state = %#v, inputs = %#v", settings, installer.inputs)
	}

	if err := controller.Reconcile(t.Context(), "g-issues"); err != nil {
		t.Fatalf("same publish Reconcile: %v", err)
	}
	if len(installer.inputs) != 1 {
		t.Fatalf("same publish retried: inputs = %#v", installer.inputs)
	}

	if err := controller.Reconcile(t.Context(), "g-issues"); err != nil {
		t.Fatalf("next publish Reconcile: %v", err)
	}
	if len(installer.inputs) != 2 || installer.inputs[1].Version != "v3" {
		t.Fatalf("next publish inputs = %#v", installer.inputs)
	}
}

type fakeReader struct {
	results     []*appregistry.AppIndexFetchResult
	errs        []error
	ifNoneMatch []string
}

func (r *fakeReader) FetchAppIndexConditional(
	_ context.Context,
	_, _ string,
	ifNoneMatch string,
) (*appregistry.AppIndexFetchResult, error) {
	r.ifNoneMatch = append(r.ifNoneMatch, ifNoneMatch)
	index := len(r.ifNoneMatch) - 1
	if index < len(r.errs) && r.errs[index] != nil {
		return nil, r.errs[index]
	}
	if index >= len(r.results) {
		return &appregistry.AppIndexFetchResult{NotModified: true}, nil
	}
	return r.results[index], nil
}

type fakeInstaller struct {
	inputs []appregistry.InstallInput
	errs   []error
}

func (i *fakeInstaller) Select(_ context.Context, input appregistry.InstallInput) (*appregistry.InstallOutput, error) {
	i.inputs = append(i.inputs, input)
	index := len(i.inputs) - 1
	if index < len(i.errs) && i.errs[index] != nil {
		return nil, i.errs[index]
	}
	return &appregistry.InstallOutput{}, nil
}

func testController(services *testutil.Services, reader RegistryReader, installer Installer) *Controller {
	return New(
		services.AutoDeploySettings,
		services.AppRollouts,
		services.AppVersionChangeRequests,
		reader,
		installer,
		map[string]AppConfig{
			"g-issues": {Registry: "toolshed", PublicRoot: "https://registry.test"},
		},
		time.Minute,
	)
}

func enableAutoDeploy(t *testing.T, services *testutil.Services, app string) {
	t.Helper()
	if _, err := services.AutoDeploySettings.Update(t.Context(), app, func(settings *core.AppAutoDeploySettings) error {
		settings.Enabled = true
		return nil
	}); err != nil {
		t.Fatalf("enable auto-deploy: %v", err)
	}
}

func testIndex(app, version string) *appregistry.Index {
	return &appregistry.Index{
		SchemaVersion: appregistry.IndexSchemaVersion,
		Apps: map[string]appregistry.AppVersions{
			app: {
				Versions: map[string]appregistry.IndexVersion{
					version: {
						Metadata:    "apps/" + app + "/versions/" + version + ".json",
						PublishedAt: time.Now().UTC(),
					},
				},
			},
		},
	}
}

var _ RegistryReader = (*fakeReader)(nil)
var _ Installer = (*fakeInstaller)(nil)
