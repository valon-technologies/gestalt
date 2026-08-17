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
	if err := probeGCSRegistryBucket(storageRoot); err != nil {
		return nil, fmt.Errorf("app registry publish storage credentials or bucket access unavailable: %w", err)
	}
	store := appregistry.NewGCSRegistryStore("gestaltd-publish")
	signer := appregistry.NewGCSUploadSigner()
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
