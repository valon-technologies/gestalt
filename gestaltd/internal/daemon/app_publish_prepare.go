package daemon

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

type prepareAppPublishReleaseInput struct {
	AppName              string
	VersionGuard         string
	DistDirs             []string
	CollectArchives      func([]string, string) (*providermanifestv1.Manifest, string, []releaseArchive, error)
	ResolveManifest      func(appName string) (absPath, relPath string, err error)
	BuildReleaseMetadata func(*providermanifestv1.Manifest, string, []releaseArchive, []byte) (*providerrelease.Metadata, error)
}

type preparedAppPublishRelease struct {
	AppName              string
	VersionGuard         string
	ReleaseManifest      *providermanifestv1.Manifest
	ReleaseVersion       string
	Archives             []releaseArchive
	ManifestPath         string
	RelativeManifestPath string
	SourceManifest       *providermanifestv1.Manifest
	ReleaseMetadata      *providerrelease.Metadata
}

func prepareAppPublishRelease(input prepareAppPublishReleaseInput) (preparedAppPublishRelease, error) {
	var zero preparedAppPublishRelease
	version := strings.TrimSpace(input.VersionGuard)
	if version == "" {
		return zero, fmt.Errorf("--version is required")
	}
	if len(input.DistDirs) == 0 {
		return zero, fmt.Errorf("--dist-dir is required")
	}
	collect := input.CollectArchives
	if collect == nil {
		collect = collectReleaseArchivesFromDirsWithProgress
	}
	releaseManifest, releaseVersion, archives, err := collect(input.DistDirs, version)
	if err != nil {
		return zero, err
	}
	appName := strings.TrimSpace(input.AppName)
	if appName == "" {
		appName, err = appregistry.AppNameFromManifestSource(releaseManifest.Source)
		if err != nil {
			return zero, fmt.Errorf("manifest source: %w", err)
		}
	}
	resolve := input.ResolveManifest
	if resolve == nil {
		resolve = func(name string) (string, string, error) {
			path, err := resolveAppPublishManifest(name)
			return path, "", err
		}
	}
	manifestPath, relManifestPath, err := resolve(appName)
	if err != nil {
		return zero, err
	}
	_, sourceManifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return zero, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	if expectedApp := strings.TrimSpace(input.AppName); expectedApp != "" {
		manifestApp, err := appregistry.AppNameFromManifestSource(sourceManifest.Source)
		if err != nil {
			return zero, fmt.Errorf("%s: invalid manifest source: %w", manifestPath, err)
		}
		if manifestApp != expectedApp {
			return zero, fmt.Errorf("%s: manifest source app %q does not match --app %q; update manifest source or pass the matching --app name", manifestPath, manifestApp, expectedApp)
		}
	}
	if err := validateProviderPublishManifest(sourceManifest, releaseManifest, releaseVersion, version); err != nil {
		return zero, err
	}
	buildMeta := input.BuildReleaseMetadata
	if buildMeta == nil {
		buildMeta = buildProviderReleaseMetadata
	}
	releaseMetadata, err := buildMeta(releaseManifest, releaseVersion, archives, nil)
	if err != nil {
		return zero, fmt.Errorf("build release metadata: %w", err)
	}
	return preparedAppPublishRelease{
		AppName:              appName,
		VersionGuard:         version,
		ReleaseManifest:      releaseManifest,
		ReleaseVersion:       releaseVersion,
		Archives:             archives,
		ManifestPath:         manifestPath,
		RelativeManifestPath: relManifestPath,
		SourceManifest:       sourceManifest,
		ReleaseMetadata:      releaseMetadata,
	}, nil
}
