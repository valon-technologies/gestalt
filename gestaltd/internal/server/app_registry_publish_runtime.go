package server

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/storage"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type AppRegistryPublishRuntime struct {
	Service *appregistry.PublishSessionService
}

func NewAppRegistryPublishRuntime(
	cfg config.AppRegistryPublishSettings,
	sessions *coredata.AppRegistryPublishSessionService,
	store appregistry.WritableRegistryStore,
	signer appregistry.RegistryUploadSigner,
	writer *appregistry.Writer,
	storageRoot string,
) (*AppRegistryPublishRuntime, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if sessions == nil {
		return nil, fmt.Errorf("app registry publish sessions store is required")
	}
	if store == nil {
		return nil, fmt.Errorf("writable app registry store is required")
	}
	if signer == nil {
		return nil, fmt.Errorf("app registry upload signer is required")
	}
	if writer == nil || writer.Store == nil {
		return nil, fmt.Errorf("app registry writer is required")
	}
	limitsCfg, err := cfg.Limits()
	if err != nil {
		return nil, err
	}
	limits := appregistry.PublishSessionLimits{
		UploadLeaseTTL:        limitsCfg.UploadLeaseTTL,
		FinalizeClaimLeaseTTL: appregistry.DefaultPublishSessionLimits().FinalizeClaimLeaseTTL,
		MaxArtifacts:          limitsCfg.MaxArtifacts,
		MaxArtifactBytes:      limitsCfg.MaxArtifactBytes,
		RequiredPlatforms:     limitsCfg.RequiredPlatforms,
	}
	indexChecker := appregistry.StoreIndexChecker{Store: store, StorageRoot: storageRoot}
	return &AppRegistryPublishRuntime{
		Service: &appregistry.PublishSessionService{
			Sessions: sessions,
			Store:    store,
			Signer:   signer,
			Writer:   writer,
			Index:    indexChecker,
			Limits:   limits,
		},
	}, nil
}

var (
	probeGCSRegistryBucketFn = probeGCSRegistryBucket
	checkUploadSigningFn     = func(signer *appregistry.GCSUploadSigner, storageRoot string) error {
		return signer.CheckSigningReadiness(context.Background(), storageRoot)
	}
)

// bootstrapAppRegistryPublish wires PublishSessionService when publish is enabled.
// Disabled publish leaves the service nil without error.
func bootstrapAppRegistryPublish(
	cfg *config.Config,
	services *coredata.Services,
) (*appregistry.PublishSessionService, error) {
	if cfg == nil || !cfg.Server.AppRegistry.Publish.Enabled {
		return nil, nil
	}
	if services == nil || services.AppRegistryPublishSessions == nil {
		return nil, fmt.Errorf("app registry publish requires coredata publish session store")
	}
	publish := cfg.Server.AppRegistry.Publish
	registryName := strings.TrimSpace(publish.WritableRegistry)
	registry, ok := cfg.AppRegistries[registryName]
	if !ok {
		return nil, fmt.Errorf("app registry publish writable registry %q is not configured", registryName)
	}
	storageRoot, err := registry.StorageURL()
	if err != nil {
		return nil, fmt.Errorf("app registry publish writable registry %q: %w", registryName, err)
	}
	if err := probeGCSRegistryBucketFn(storageRoot); err != nil {
		return nil, fmt.Errorf("app registry publish storage credentials or bucket access unavailable: %w", err)
	}
	signer := appregistry.NewGCSUploadSigner()
	if err := checkUploadSigningFn(signer, storageRoot); err != nil {
		return nil, err
	}
	store := appregistry.NewGCSRegistryStore("gestaltd-publish")
	unusedRetention, deployedRetention, err := registry.RetentionPolicy()
	if err != nil {
		return nil, fmt.Errorf("app registry publish retention policy: %w", err)
	}
	writer := &appregistry.Writer{
		Store: store,
		RetentionPolicy: appregistry.RetentionPolicy{
			UnusedRetention:   unusedRetention,
			DeployedRetention: deployedRetention,
		},
	}
	runtime, err := NewAppRegistryPublishRuntime(
		publish,
		services.AppRegistryPublishSessions,
		store,
		signer,
		writer,
		storageRoot,
	)
	if err != nil {
		return nil, err
	}
	return runtime.Service, nil
}

func probeGCSRegistryBucket(storageRoot string) error {
	bucket := strings.TrimPrefix(strings.TrimSpace(storageRoot), "gs://")
	bucket = strings.Trim(bucket, "/")
	if bucket == "" || strings.Contains(bucket, "/") {
		return fmt.Errorf("invalid storage root %q", storageRoot)
	}
	client, err := storage.NewClient(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	_, err = client.Bucket(bucket).Attrs(context.Background())
	return err
}

func publishSessionLedger(services *coredata.Services) *coredata.AppRegistryPublishSessionService {
	if services == nil {
		return nil
	}
	return services.AppRegistryPublishSessions
}

// BootstrapAppRegistryPublishForTest exposes publish bootstrap for server tests.
func BootstrapAppRegistryPublishForTest(cfg *config.Config, services *coredata.Services) (*appregistry.PublishSessionService, error) {
	return bootstrapAppRegistryPublish(cfg, services)
}

// ProbeGCSRegistryBucketForTest returns the current bucket probe hook.
func ProbeGCSRegistryBucketForTest() func(string) error {
	return probeGCSRegistryBucketFn
}

// SetProbeGCSRegistryBucketForTest replaces the bucket probe hook in tests.
func SetProbeGCSRegistryBucketForTest(fn func(string) error) {
	probeGCSRegistryBucketFn = fn
}

// CheckUploadSigningForTest returns the current upload signing readiness hook.
func CheckUploadSigningForTest() func(*appregistry.GCSUploadSigner, string) error {
	return checkUploadSigningFn
}

// SetCheckUploadSigningForTest replaces the upload signing readiness hook in tests.
func SetCheckUploadSigningForTest(fn func(*appregistry.GCSUploadSigner, string) error) {
	checkUploadSigningFn = fn
}
