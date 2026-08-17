package server

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/storage"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

var (
	probeGCSRegistryBucketFn = probeGCSRegistryBucket
	checkUploadSigningFn     = func(signer *appregistry.GCSUploadSigner, storageRoot string) error {
		return signer.CheckSigningReadiness(context.Background(), storageRoot)
	}
)

func bootstrapAppRegistryPublish(cfg *config.Config) (*appregistry.StatelessPublishService, error) {
	if cfg == nil || !cfg.Server.AppRegistry.Publish.Enabled {
		return nil, nil
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
	limitsCfg, err := publish.Limits()
	if err != nil {
		return nil, err
	}
	return &appregistry.StatelessPublishService{
		Store:  store,
		Signer: signer,
		Writer: writer,
		Index:  appregistry.StoreIndexChecker{Store: store, StorageRoot: storageRoot},
		Limits: appregistry.PublishLimits{
			UploadURLTTL:      limitsCfg.UploadURLTTL,
			MaxArtifacts:      limitsCfg.MaxArtifacts,
			MaxArtifactBytes:  limitsCfg.MaxArtifactBytes,
			RequiredPlatforms: limitsCfg.RequiredPlatforms,
		},
	}, nil
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

func BootstrapAppRegistryPublishForTest(cfg *config.Config) (*appregistry.StatelessPublishService, error) {
	return bootstrapAppRegistryPublish(cfg)
}

func ProbeGCSRegistryBucketForTest() func(string) error {
	return probeGCSRegistryBucketFn
}

func SetProbeGCSRegistryBucketForTest(fn func(string) error) {
	probeGCSRegistryBucketFn = fn
}

func CheckUploadSigningForTest() func(*appregistry.GCSUploadSigner, string) error {
	return checkUploadSigningFn
}

func SetCheckUploadSigningForTest(fn func(*appregistry.GCSUploadSigner, string) error) {
	checkUploadSigningFn = fn
}
