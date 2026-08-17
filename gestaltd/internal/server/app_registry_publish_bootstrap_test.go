package server_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestBootstrapAppRegistryPublishDisabledSkipsSigningReadiness(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	service, err := bootstrapAppRegistryPublishForTest(&config.Config{}, services)
	if err != nil {
		t.Fatalf("bootstrap disabled: %v", err)
	}
	if service != nil {
		t.Fatal("expected nil publish service when disabled")
	}
}

func TestBootstrapAppRegistryPublishFailsWhenSigningUnavailable(t *testing.T) {
	t.Parallel()

	registry, err := config.NewGCSAppRegistry("gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}
	services := testutil.NewStubServices(t)
	cfg := &config.Config{
		AppRegistries: map[string]config.AppRegistryConfig{"toolshed": registry},
	}
	cfg.Server.AppRegistry.Publish.Enabled = true
	cfg.Server.AppRegistry.Publish.WritableRegistry = "toolshed"

	restoreProbe := stubProbeGCSRegistryBucket(t, nil)
	defer restoreProbe()
	restoreSigning := stubCheckUploadSigning(t, fmt.Errorf("signBlob unavailable"))
	defer restoreSigning()

	_, err = bootstrapAppRegistryPublishForTest(cfg, services)
	if err == nil || !strings.Contains(err.Error(), "signBlob unavailable") {
		t.Fatalf("bootstrap error = %v", err)
	}
}

func TestBootstrapAppRegistryPublishFailsWhenBucketUnavailable(t *testing.T) {
	t.Parallel()

	registry, err := config.NewGCSAppRegistry("gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}
	services := testutil.NewStubServices(t)
	cfg := &config.Config{
		AppRegistries: map[string]config.AppRegistryConfig{"toolshed": registry},
	}
	cfg.Server.AppRegistry.Publish.Enabled = true
	cfg.Server.AppRegistry.Publish.WritableRegistry = "toolshed"

	restoreProbe := stubProbeGCSRegistryBucket(t, errors.New("bucket unavailable"))
	defer restoreProbe()

	_, err = bootstrapAppRegistryPublishForTest(cfg, services)
	if err == nil || !strings.Contains(err.Error(), "bucket unavailable") {
		t.Fatalf("bootstrap error = %v", err)
	}
}

func bootstrapAppRegistryPublishForTest(cfg *config.Config, services *coredata.Services) (*appregistry.PublishSessionService, error) {
	return server.BootstrapAppRegistryPublishForTest(cfg, services)
}

func stubProbeGCSRegistryBucket(t *testing.T, err error) func() {
	t.Helper()
	prev := server.ProbeGCSRegistryBucketForTest()
	server.SetProbeGCSRegistryBucketForTest(func(string) error { return err })
	return func() { server.SetProbeGCSRegistryBucketForTest(prev) }
}

func stubCheckUploadSigning(t *testing.T, err error) func() {
	t.Helper()
	prev := server.CheckUploadSigningForTest()
	server.SetCheckUploadSigningForTest(func(*appregistry.GCSUploadSigner, string) error { return err })
	return func() { server.SetCheckUploadSigningForTest(prev) }
}
