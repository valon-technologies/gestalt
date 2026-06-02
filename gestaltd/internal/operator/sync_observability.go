package operator

import (
	"fmt"
	"strconv"

	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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

type PreparedArtifactRoot struct {
	Subject string
	Kind    string
	Name    string
	DestDir string
}

func preparedArtifactRoots(paths lifecyclePaths, cfg *config.Config) []PreparedArtifactRoot {
	if cfg == nil {
		return nil
	}
	var roots []PreparedArtifactRoot
	for name, entry := range cfg.Apps {
		if entry != nil {
			roots = append(roots, PreparedArtifactRoot{
				Subject: "provider " + strconv.Quote(name),
				Kind:    providermanifestv1.KindApp,
				Name:    name,
				DestDir: providerDestDir(paths, name),
			})
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		kind := providerManifestKind(collection.kind)
		for name, entry := range collection.entries {
			if entry != nil {
				roots = append(roots, PreparedArtifactRoot{
					Subject: fmt.Sprintf("%s %q", kind, name),
					Kind:    kind,
					Name:    name,
					DestDir: componentDestDir(paths, collection.kind, name),
				})
			}
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry != nil {
			roots = append(roots, PreparedArtifactRoot{
				Subject: fmt.Sprintf("%s %q", providermanifestv1.KindRuntime, name),
				Kind:    providermanifestv1.KindRuntime,
				Name:    name,
				DestDir: runtimeDestDir(paths, name),
			})
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if entry != nil {
			roots = append(roots, PreparedArtifactRoot{
				Subject: fmt.Sprintf("%s %q", providermanifestv1.KindIndexedDB, name),
				Kind:    providermanifestv1.KindIndexedDB,
				Name:    name,
				DestDir: indexeddbDestDir(paths, name),
			})
		}
	}
	for name, entry := range cfg.Providers.S3 {
		if entry != nil {
			roots = append(roots, PreparedArtifactRoot{
				Subject: fmt.Sprintf("%s %q", providermanifestv1.KindS3, name),
				Kind:    providermanifestv1.KindS3,
				Name:    name,
				DestDir: s3DestDir(paths, name),
			})
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry != nil {
			roots = append(roots, PreparedArtifactRoot{
				Subject: "ui " + strconv.Quote(name),
				Kind:    providermanifestv1.KindUI,
				Name:    name,
				DestDir: uiDestDir(paths, name),
			})
		}
	}
	return dedupePreparedArtifactRoots(roots)
}
