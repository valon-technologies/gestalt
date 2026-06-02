package operator

import (
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

type SyncObservability struct {
	Recorder    *SyncMetricsRecorder
	BuildOutput providerpkg.CommandOutput
}

func (paths lifecyclePaths) stageOptions() providerpkg.StageSourcePreparedInstallOptions {
	return providerpkg.StageSourcePreparedInstallOptions{
		BuildOutput: paths.syncBuildOutput,
	}
}

func preparedArtifactDestDirs(paths lifecyclePaths, cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	var roots []string
	for name, entry := range cfg.Apps {
		if entry != nil {
			roots = append(roots, providerDestDir(paths, name))
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for name, entry := range collection.entries {
			if entry != nil {
				roots = append(roots, componentDestDir(paths, collection.kind, name))
			}
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry != nil {
			roots = append(roots, runtimeDestDir(paths, name))
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if entry != nil {
			roots = append(roots, indexeddbDestDir(paths, name))
		}
	}
	for name, entry := range cfg.Providers.S3 {
		if entry != nil {
			roots = append(roots, s3DestDir(paths, name))
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry != nil {
			roots = append(roots, uiDestDir(paths, name))
		}
	}
	return dedupeCleanPaths(roots)
}
