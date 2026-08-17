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
	cfg config.AppRegistryPublishConfig,
	sessions *coredata.AppRegistryPublishSessionService,
	store appregistry.WritableRegistryStore,
	signer appregistry.RegistryUploadSigner,
	reader *appregistry.RegistryReader,
	retention appregistry.RetentionPolicy,
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
	indexChecker := appregistry.RegistryReaderIndexChecker{Reader: reader}
	writer := &appregistry.Writer{
		Store:           store,
		RetentionPolicy: retention,
	}
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
