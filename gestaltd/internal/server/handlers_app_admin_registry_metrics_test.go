package server_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestAppAdminRegistryAutoDeployToggleAfterPauseRecordsResumedMetric(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	metrics := metrictest.NewManualMeterProvider(t)
	services := testutil.NewStubServices(t)
	if _, err := services.AutoDeploySettings.Update(context.Background(), "g-issues", func(settings *core.AppAutoDeploySettings) error {
		settings.Enabled = true
		settings.Paused = true
		settings.PauseReason = core.AppAutoDeployPauseReasonRolloutFailed
		settings.LastError = "Automatic deployment is paused because rollout of version v1 failed. Select that version again to retry."
		return nil
	}); err != nil {
		t.Fatalf("seed paused auto-deploy settings: %v", err)
	}
	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.Services = services
		cfg.MeterProvider = metrics.Provider
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": fixture.Registry}
		cfg.AppRegistryReader = fixture.Reader
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(
		http.MethodPut,
		ts.URL+"/api/v1/apps/g-issues/admin/registry/auto-deploy",
		bytes.NewBufferString(`{"enabled":true}`),
	)
	request.Header.Set("Authorization", "Bearer alice-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("PUT auto-deploy: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("PUT status = %d, want %d: %s", response.StatusCode, http.StatusOK, body)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.appregistry.autodeploy.resumed_count", 1, map[string]string{
		"gestaltd.appregistry.app":                "g-issues",
		"gestaltd.appregistry.autodeploy_trigger": "toggle",
	})
}
