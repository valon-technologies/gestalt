package appregistry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

type materializeRegistryVersionInput struct {
	registryName string
	appName      string
	version      string
}

type materializeRegistryVersionResult struct {
	materializedPath string
	entry            *Entry
	entryURL         string
	checksums        map[string]string
}

// LocalMaterializedPath returns the on-disk path for one registry-installed app version.
func LocalMaterializedPath(artifactsDir, appName, version string) string {
	artifactsDir = strings.TrimSpace(artifactsDir)
	appName = strings.TrimSpace(appName)
	version = strings.TrimSpace(version)
	if artifactsDir == "" || appName == "" || version == "" {
		return ""
	}
	return filepath.Join(artifactsDir, RegistryInstallSubdir, appName, version)
}

// IsLocallyMaterialized reports whether destDir contains an extracted provider manifest.
func IsLocallyMaterialized(destDir string) bool {
	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return false
	}
	if _, err := os.Stat(destDir); err != nil {
		return false
	}
	manifestPath, _ := packageio.FindManifestFile(destDir)
	return strings.TrimSpace(manifestPath) != ""
}

func (i *Installer) materializeRegistryVersion(ctx context.Context, input materializeRegistryVersionInput) (*materializeRegistryVersionResult, error) {
	if i == nil {
		return nil, fmt.Errorf("app registry installer is not configured")
	}
	registryName := strings.TrimSpace(input.registryName)
	appName := strings.TrimSpace(input.appName)
	version := strings.TrimSpace(input.version)
	if registryName == "" {
		return nil, fmt.Errorf("registry is required")
	}
	if appName == "" {
		return nil, fmt.Errorf("app is required")
	}
	if version == "" {
		return nil, fmt.Errorf("version is required")
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		return nil, fmt.Errorf("invalid app name: %w", err)
	}
	artifactsDir := strings.TrimSpace(i.ArtifactsDir)
	if artifactsDir == "" {
		return nil, fmt.Errorf("artifacts directory is not configured")
	}
	registry, ok := i.Registries[registryName]
	if !ok {
		return nil, fmt.Errorf("app registry not found")
	}
	if strings.TrimSpace(registry.Kind) != config.AppRegistryKindGCS {
		return nil, fmt.Errorf("unsupported app registry kind")
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		return nil, fmt.Errorf("app registry public URL is invalid: %w", err)
	}

	reader := i.Reader
	if reader == nil {
		reader = &RegistryReader{}
	}
	entry, err := reader.FetchEntry(ctx, publicRoot, appName, version)
	if err != nil {
		return nil, fmt.Errorf("fetch app registry entry: %w", err)
	}
	if entry.App != appName {
		return nil, fmt.Errorf("registry entry app %q does not match requested app %q", entry.App, appName)
	}
	if entry.Version != version {
		return nil, fmt.Errorf("registry entry version %q does not match requested version %q", entry.Version, version)
	}

	platform := providerpkg.CurrentPlatformString()
	artifact, ok := entry.Artifacts[platform]
	if !ok {
		return nil, fmt.Errorf("registry entry has no artifact for platform %q", platform)
	}
	artifactURL := strings.TrimSpace(artifact.PublicURL)
	if artifactURL == "" {
		artifactURL = strings.TrimSpace(artifact.URL)
	}
	if artifactURL == "" {
		return nil, fmt.Errorf("registry entry artifact for platform %q has no download URL", platform)
	}
	expectedSHA := strings.TrimSpace(artifact.SHA256)
	if expectedSHA == "" {
		return nil, fmt.Errorf("registry entry artifact for platform %q is missing sha256", platform)
	}

	materializedPath := LocalMaterializedPath(artifactsDir, appName, version)
	download, err := downloadRegistryArtifact(ctx, reader.client(), artifactURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		if download.Cleanup != nil {
			download.Cleanup()
		}
	}()
	if !strings.EqualFold(strings.TrimSpace(download.SHA256Hex), expectedSHA) {
		return nil, fmt.Errorf("artifact digest mismatch: got %s, want %s", download.SHA256Hex, expectedSHA)
	}
	if err := i.materializePublishedPackage(ctx, download.LocalPath, materializedPath, appName); err != nil {
		return nil, fmt.Errorf("materialize app artifact: %w", err)
	}

	return &materializeRegistryVersionResult{
		materializedPath: materializedPath,
		entry:            entry,
		entryURL:         PublicURL(publicRoot, AppVersionEntryPath(appName, version)),
		checksums:        map[string]string{platform: expectedSHA},
	}, nil
}

func (i *Installer) materializeKnownVersion(ctx context.Context, installation *core.AppInstallation) (string, error) {
	if installation == nil {
		return "", fmt.Errorf("installation is required")
	}
	appName := strings.TrimSpace(installation.AppName)
	version := strings.TrimSpace(installation.Version)
	registryName := strings.TrimSpace(installation.Registry)
	if appName == "" || version == "" {
		return "", fmt.Errorf("app and version are required")
	}
	if registryName == "" {
		return "", fmt.Errorf("registry is required")
	}

	materializedPath := LocalMaterializedPath(i.ArtifactsDir, appName, version)
	if IsLocallyMaterialized(materializedPath) {
		return materializedPath, nil
	}

	result, err := i.materializeRegistryVersion(ctx, materializeRegistryVersionInput{
		registryName: registryName,
		appName:      appName,
		version:      version,
	})
	if err != nil {
		return "", err
	}
	return result.materializedPath, nil
}
