package daemon

import (
	"os"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/internal/daemon/appregistryremote"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func init() {
	appregistryremote.RegisterPublishHelpers(appregistryremote.PublishHelpers{
		CollectReleaseArchivesFromDirs:  collectReleaseArchivesFromDirsForRemote,
		BuildProviderReleaseMetadata:    buildProviderReleaseMetadataForRemote,
		ValidateProviderPublishManifest: validateProviderPublishManifest,
		ResolvePublishManifest:          resolveRemotePublishManifest,
	})
}

func collectReleaseArchivesFromDirsForRemote(distDirs []string, version string) (*providermanifestv1.Manifest, string, []appregistryremote.DaemonReleaseArchive, error) {
	manifest, releaseVersion, archives, err := collectReleaseArchivesFromDirsWithProgress(distDirs, version)
	if err != nil {
		return nil, "", nil, err
	}
	out := make([]appregistryremote.DaemonReleaseArchive, 0, len(archives))
	for _, archive := range archives {
		size := int64(0)
		if info, statErr := os.Stat(archive.Path); statErr == nil {
			size = info.Size()
		}
		out = append(out, appregistryremote.DaemonReleaseArchive{Path: archive.Path, SHA256: archive.SHA256, Target: archive.Target, Size: size})
	}
	return manifest, releaseVersion, out, nil
}

func buildProviderReleaseMetadataForRemote(manifest *providermanifestv1.Manifest, version string, archives []appregistryremote.DaemonReleaseArchive, rawManifest []byte) (*providerrelease.Metadata, error) {
	releaseArchives := make([]releaseArchive, 0, len(archives))
	for _, archive := range archives {
		releaseArchives = append(releaseArchives, releaseArchive{Path: archive.Path, SHA256: archive.SHA256, Target: archive.Target})
	}
	return buildProviderReleaseMetadata(manifest, version, releaseArchives, rawManifest)
}

func resolveRemotePublishManifest(appName string) (string, string, error) {
	root, err := gitRootFromWorkingDirectory()
	if err != nil {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			root = cwd
		} else {
			return "", "", err
		}
	}
	path, err := resolveAppPublishManifestFromGitRoot(root, appName)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", "", err
	}
	return path, filepath.ToSlash(rel), nil
}
