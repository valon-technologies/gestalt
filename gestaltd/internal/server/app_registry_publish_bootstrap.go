package server

import (
	"fmt"

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
		UploadLeaseTTL:    limitsCfg.UploadLeaseTTL,
		MaxArtifacts:      limitsCfg.MaxArtifacts,
		MaxArtifactBytes:  limitsCfg.MaxArtifactBytes,
		RequiredPlatforms: limitsCfg.RequiredPlatforms,
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
